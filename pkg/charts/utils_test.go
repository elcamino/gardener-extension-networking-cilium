package charts

import (
	"strings"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/chartrenderer"
	"github.com/gardener/gardener/pkg/extensions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/apimachinery/pkg/version"

	"github.com/gardener/gardener-extension-networking-cilium/charts"
	ciliumv1alpha1 "github.com/gardener/gardener-extension-networking-cilium/pkg/apis/cilium/v1alpha1"
	"github.com/gardener/gardener-extension-networking-cilium/pkg/cilium"
)

var _ = Describe("#applyEncryptionConfig", func() {
	Describe("wireguard", func() {
		var config *ciliumv1alpha1.NetworkConfig
		BeforeEach(func() {
			config = &ciliumv1alpha1.NetworkConfig{
				Encryption: &ciliumv1alpha1.Encryption{
					Mode:    ciliumv1alpha1.EncryptionModeWireguard,
					Enabled: true,
				},
			}
		})
		Describe("vxlan tunnel config with strict mode encryption", func() {
			It("should set AllowRemoteNodeIdentities", func() {
				cfg := &globalConfig{
					Tunnel: ciliumv1alpha1.VXLan,
				}
				config.Encryption.StrictMode = true
				Expect(applyEncryptionConfig(cfg, config)).ShouldNot(HaveOccurred())
				Expect(cfg.Encryption.Wireguard.StrictMode.AllowRemoteNodeIdentities).To(BeTrue())
			})
		})
		Describe("overlapping node & pod CIDR with direct routing and strict mode encryption", func() {
			It("should set AllowRemoteNodeIdentities", func() {
				config.Encryption.StrictMode = true
				config.Overlay = &ciliumv1alpha1.Overlay{
					Enabled: false,
				}
				cfg := &globalConfig{
					Tunnel:   ciliumv1alpha1.Disabled,
					PodCIDR:  "10.0.0.0/16",
					NodeCIDR: "10.0.0.128/17",
				}
				Expect(applyEncryptionConfig(cfg, config)).ShouldNot(HaveOccurred())
				Expect(cfg.Encryption.Wireguard.StrictMode.AllowRemoteNodeIdentities).To(BeTrue())
			})
		})
	})
})

var _ = Describe("#ComputeMonitoringChartValues", func() {
	Describe("hubble toggle", func() {
		It("should marshal hubble enabled as a nested values map", func() {
			values, err := ComputeMonitoringChartValues(true)
			Expect(err).NotTo(HaveOccurred())
			Expect(values).To(Equal(map[string]any{"hubble": map[string]any{"enabled": true}}))
		})

		It("should marshal hubble disabled as a nested values map", func() {
			values, err := ComputeMonitoringChartValues(false)
			Expect(err).NotTo(HaveOccurred())
			Expect(values).To(Equal(map[string]any{"hubble": map[string]any{"enabled": false}}))
		})
	})
})

var _ = Describe("#generateChartValues", func() {
	Describe("hubble", func() {
		var config *ciliumv1alpha1.NetworkConfig
		cluster := &extensions.Cluster{
			Shoot: &gardencorev1beta1.Shoot{},
		}
		BeforeEach(func() {
			config = &ciliumv1alpha1.NetworkConfig{}
		})
		Describe("reflect hubble of NetworkConfig in both requirementsConfig and globalConfig", func() {
			It("should be disabled in requirementsConfig and globalConfig", func() {
				config.Hubble = &ciliumv1alpha1.Hubble{
					Enabled: false,
				}
				requirementCfg, globalCfg, err := generateChartValues(config, &extensionsv1alpha1.Network{}, cluster, "", "", "")
				Expect(err).NotTo(HaveOccurred())
				Expect(requirementCfg.Hubble.Enabled).To(Equal(config.Hubble.Enabled), "requirementsConfig mismatch")
				Expect(globalCfg.Hubble.Enabled).To(Equal(config.Hubble.Enabled), "globalConfig mismatch")
			})
		})
	})
})

var _ = Describe("cilium-monitoring chart", func() {
	Describe("hubble scrape config", func() {
		render := func(hubbleEnabled bool) string {
			renderer := chartrenderer.NewWithServerVersion(&version.Info{Major: "1", Minor: "31"})

			values, err := ComputeMonitoringChartValues(hubbleEnabled)
			Expect(err).NotTo(HaveOccurred())

			release, err := renderer.RenderEmbeddedFS(charts.InternalChart, cilium.CiliumMonitoringChartPath, cilium.MonitoringName, "shoot--foo--bar", values)
			Expect(err).NotTo(HaveOccurred())

			return string(release.Manifest())
		}

		It("should render the hubble scrape config if hubble is enabled", func() {
			Expect(render(true)).To(ContainSubstring("name: " + cilium.HubbleScrapeConfigName))
		})

		It("should not render the hubble scrape config if hubble is disabled", func() {
			manifest := render(false)
			Expect(manifest).NotTo(ContainSubstring("name: " + cilium.HubbleScrapeConfigName))
			Expect(manifest).To(ContainSubstring("name: " + cilium.AgentScrapeConfigName))
		})
	})
})

// scrapeAnnotatedCiliumServices returns the names of all Services in the given manifest which carry
// the annotation prometheus.io/scrape=true and select the cilium agent pods. These Services are the
// only source of Endpoints for the endpoint based ScrapeConfigs of the cilium-monitoring chart, so
// at least one of them must exist independently of the Hubble feature toggle.
func scrapeAnnotatedCiliumServices(manifest string) []string {
	var names []string

	for _, document := range strings.Split(manifest, "\n---") {
		service := &corev1.Service{}
		if err := yaml.Unmarshal([]byte(document), service); err != nil {
			continue
		}

		if service.Kind != "Service" {
			continue
		}

		if service.Annotations["prometheus.io/scrape"] == "true" && service.Spec.Selector["k8s-app"] == "cilium" {
			names = append(names, service.Name)
		}
	}

	return names
}

var _ = Describe("#RenderCiliumChart", func() {
	Describe("scrape discovery of the cilium agent", func() {
		render := func(hubbleEnabled bool) string {
			renderer := chartrenderer.NewWithServerVersion(&version.Info{Major: "1", Minor: "31"})
			cluster := &extensions.Cluster{Shoot: &gardencorev1beta1.Shoot{}}
			config := &ciliumv1alpha1.NetworkConfig{Hubble: &ciliumv1alpha1.Hubble{Enabled: hubbleEnabled}}

			manifest, err := RenderCiliumChart(renderer, config, &extensionsv1alpha1.Network{}, cluster, "", "", "")
			Expect(err).NotTo(HaveOccurred())

			return string(manifest)
		}

		It("should keep a scrape annotated service in front of the cilium pods if hubble is enabled", func() {
			Expect(scrapeAnnotatedCiliumServices(render(true))).NotTo(BeEmpty())
		})

		It("should keep a scrape annotated service in front of the cilium pods if hubble is disabled", func() {
			Expect(scrapeAnnotatedCiliumServices(render(false))).NotTo(BeEmpty(),
				"no service with prometheus.io/scrape=true selects the cilium agent pods, the %s scrape config will not find any target", cilium.AgentScrapeConfigName)
		})
	})
})

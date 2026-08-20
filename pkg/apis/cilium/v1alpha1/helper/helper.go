// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package helper

import (
	ciliumv1alpha1 "github.com/gardener/gardener-extension-networking-cilium/pkg/apis/cilium/v1alpha1"
)

// HubbleEnabled returns true if Hubble is enabled in the given NetworkConfig.
func HubbleEnabled(config *ciliumv1alpha1.NetworkConfig) bool {
	return config != nil && config.Hubble != nil && config.Hubble.Enabled
}

// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package configdump

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"github.com/golang/protobuf/ptypes"

	protio "istio.io/istio/istioctl/pkg/util/proto"
	"istio.io/istio/pilot/pkg/networking/util"
	v3 "istio.io/istio/pilot/pkg/proxy/envoy/v3"
)

const (
	// HTTPListener identifies a listener as being of HTTP type by the presence of an HTTP connection manager filter
	HTTPListener = "envoy.http_connection_manager"

	// TCPListener identifies a listener as being of TCP type by the presence of TCP proxy filter
	TCPListener = "envoy.tcp_proxy"
)

// ListenerFilter is used to pass filter information into listener based config writer print functions
type ListenerFilter struct {
	Address string
	Port    uint32
	Type    string
}

// Verify returns true if the passed listener matches the filter fields
func (l *ListenerFilter) Verify(listener *listener.Listener) bool {
	if l.Address == "" && l.Port == 0 && l.Type == "" {
		return true
	}
	if l.Address != "" && !strings.EqualFold(retrieveListenerAddress(listener), l.Address) {
		return false
	}
	if l.Port != 0 && retrieveListenerPort(listener) != l.Port {
		return false
	}
	if l.Type != "" && !strings.EqualFold(retrieveListenerType(listener), l.Type) {
		return false
	}
	return true
}

// retrieveListenerType classifies a Listener as HTTP|TCP|HTTP+TCP|UNKNOWN
func retrieveListenerType(l *listener.Listener) string {
	nHTTP := 0
	nTCP := 0
	for _, filterChain := range l.GetFilterChains() {
		for _, filter := range filterChain.GetFilters() {
			if filter.Name == HTTPListener {
				nHTTP++
			} else if filter.Name == TCPListener {
				if !strings.Contains(string(filter.GetTypedConfig().GetValue()), util.BlackHoleCluster) {
					nTCP++
				}
			}
		}
	}

	if nHTTP > 0 {
		if nTCP == 0 {
			return "HTTP"
		}
		return "HTTP+TCP"
	} else if nTCP > 0 {
		return "TCP"
	}

	return "UNKNOWN"
}

func retrieveListenerAddress(l *listener.Listener) string {
	return l.Address.GetSocketAddress().Address
}

func retrieveListenerPort(l *listener.Listener) uint32 {
	return l.Address.GetSocketAddress().GetPortValue()
}

// PrintListenerSummary prints a summary of the relevant listeners in the config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintListenerSummary(filter ListenerFilter) error {
	w, listeners, err := c.setupListenerConfigWriter()
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "ADDRESS\tPORT\tTYPE")
	for _, listener := range listeners {
		if filter.Verify(listener) {
			address := retrieveListenerAddress(listener)
			port := retrieveListenerPort(listener)
			listenerType := retrieveListenerType(listener)
			fmt.Fprintf(w, "%v\t%v\t%v\n", address, port, listenerType)
		}
	}
	return w.Flush()
}

// PrintListenerDump prints the relevant listeners in the config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintListenerDump(filter ListenerFilter) error {
	_, listeners, err := c.setupListenerConfigWriter()
	if err != nil {
		return err
	}
	filteredListeners := protio.MessageSlice{}
	for _, listener := range listeners {
		if filter.Verify(listener) {
			filteredListeners = append(filteredListeners, listener)
		}
	}
	out, err := json.MarshalIndent(filteredListeners, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal listeners: %v", err)
	}
	fmt.Fprintln(c.Stdout, string(out))
	return nil
}

func (c *ConfigWriter) setupListenerConfigWriter() (*tabwriter.Writer, []*listener.Listener, error) {
	listeners, err := c.retrieveSortedListenerSlice()
	if err != nil {
		return nil, nil, err
	}
	w := new(tabwriter.Writer).Init(c.Stdout, 0, 8, 5, ' ', 0)
	return w, listeners, nil
}

func (c *ConfigWriter) retrieveSortedListenerSlice() ([]*listener.Listener, error) {
	if c.configDump == nil {
		return nil, fmt.Errorf("config writer has not been primed")
	}
	listenerDump, err := c.configDump.GetListenerConfigDump()
	if err != nil {
		return nil, fmt.Errorf("listener dump: %v", err)
	}
	listeners := make([]*listener.Listener, 0)
	for _, l := range listenerDump.DynamicListeners {
		if l.ActiveState != nil && l.ActiveState.Listener != nil {
			listenerTyped := &listener.Listener{}
			// Support v2 or v3 in config dump. See ads.go:RequestedTypes for more info.
			l.ActiveState.Listener.TypeUrl = v3.ListenerType
			err = ptypes.UnmarshalAny(l.ActiveState.Listener, listenerTyped)
			if err != nil {
				return nil, fmt.Errorf("unmarshal listener: %v", err)
			}
			listeners = append(listeners, listenerTyped)
		}
	}

	for _, l := range listenerDump.StaticListeners {
		if l.Listener != nil {
			listenerTyped := &listener.Listener{}
			// Support v2 or v3 in config dump. See ads.go:RequestedTypes for more info.
			l.Listener.TypeUrl = v3.ListenerType
			err = ptypes.UnmarshalAny(l.Listener, listenerTyped)
			if err != nil {
				return nil, fmt.Errorf("unmarshal listener: %v", err)
			}
			listeners = append(listeners, listenerTyped)
		}
	}
	if len(listeners) == 0 {
		return nil, fmt.Errorf("no listeners found")
	}
	return listeners, nil
}

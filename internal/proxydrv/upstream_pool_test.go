package proxydrv

import "testing"

func oneRoute(ip string) RouteSet {
	return RouteSet{Routes: []ProxyRoute{{
		Domain:    "app.example.com",
		Upstreams: []ProxyUpstream{{IP: ip, Port: 8080}},
	}}}
}

// Docker'ın varsayılan köprü ağları /16'dır: x.y.0.0 ağ adresi, x.y.0.1
// ağ geçidi (HOST'un kendisi), x.y.255.255 yayın adresi. Ele geçmiş bir
// kadrand bunlardan birini upstream diye gönderirse Caddy trafiği hosta
// yönlendirirdi (B2/I6).
func TestFixedConfigRejectsDockerNetworkInfrastructureAddresses(t *testing.T) {
	for _, ip := range []string{
		"172.17.0.0", "172.17.0.1", "172.17.255.255",
		"172.18.0.0", "172.18.0.1", "172.18.255.255",
		"172.31.0.1", "172.31.255.255", "172.16.0.0", "172.16.0.1",
	} {
		if _, err := FixedConfig(oneRoute(ip)); err == nil {
			t.Errorf("%s accepted as an upstream", ip)
		}
	}
}

// Kontrol grubu: ağ geçidinin yanındaki ve /16 içindeki sıradan adresler
// kabul edilmeli; yoksa kural meşru konteynerleri de düşürürdü.
func TestFixedConfigAcceptsOrdinaryContainerAddresses(t *testing.T) {
	for _, ip := range []string{
		"172.18.0.2", "172.18.0.3", "172.18.1.0", "172.18.1.1",
		"172.18.1.255", "172.18.255.254", "172.31.255.254",
	} {
		if _, err := FixedConfig(oneRoute(ip)); err != nil {
			t.Errorf("%s rejected: %v", ip, err)
		}
	}
}

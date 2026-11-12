package dockerdrv

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// Usage, bir konteynerin kümülatif kaynak sayaçları (K-166). Hızı istemci
// iki ölçümün farkından hesaplar.
type Usage struct {
	CPUNanos, MemoryBytes, MemoryLimit uint64
	RxBytes, TxBytes                   uint64
	ReadBytes, WriteBytes              uint64
	Tasks                              uint32
}

// statsBody, `GET /containers/{id}/stats` yanıtının kullanılan alanları.
// Dar yapı, listEntry ile aynı gerekçe (K-002).
type statsBody struct {
	CPU struct {
		Usage struct {
			Total uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
	} `json:"cpu_stats"`
	Memory struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
	Pids struct {
		Current uint32 `json:"current"`
	} `json:"pids_stats"`
	Networks map[string]struct {
		Rx uint64 `json:"rx_bytes"`
		Tx uint64 `json:"tx_bytes"`
	} `json:"networks"`
	Blkio struct {
		Bytes []struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		} `json:"io_service_bytes_recursive"`
	} `json:"blkio_stats"`
}

// ContainerStats, bir konteynerin tek seferlik istatistiği. Kimlik
// yalnız sürücünün KENDİ listesinden gelir (ContainerList); dışarıdan
// kimlik alan bir yol yok.
//
// one-shot: Docker önceki CPU örneği için bir saniye beklemesin; hızı
// zaten istemci hesaplıyor.
func (c *Client) ContainerStats(ctx context.Context, id string) (Usage, error) {
	var b statsBody
	q := url.Values{"stream": {"false"}, "one-shot": {"true"}}
	if err := c.doJSON(ctx, http.MethodGet, "/containers/"+url.PathEscape(id)+"/stats", q, nil, &b); err != nil {
		return Usage{}, err
	}
	u := Usage{CPUNanos: b.CPU.Usage.Total, MemoryBytes: b.Memory.Usage, MemoryLimit: b.Memory.Limit, Tasks: b.Pids.Current}
	// docker stats ile aynı hesap: etkin olmayan dosya önbelleği çekirdeğin
	// geri alabileceği bellek, kullanılan değil.
	if cache := b.Memory.Stats["inactive_file"]; cache <= u.MemoryBytes {
		u.MemoryBytes -= cache
	}
	for _, n := range b.Networks {
		u.RxBytes += n.Rx
		u.TxBytes += n.Tx
	}
	// İşlem adı cgroup v1'de "Read", v2'de "read".
	for _, e := range b.Blkio.Bytes {
		switch strings.ToLower(e.Op) {
		case "read":
			u.ReadBytes += e.Value
		case "write":
			u.WriteBytes += e.Value
		}
	}
	return u, nil
}

package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// The SeaweedFS calls core makes over its HTTP API: the master's
// /dir/status (the topology: volume slots, data nodes, each bucket's
// writable volumes), /vol/status (every volume, as the volume servers last
// said), /cluster/status (the leader) and /vol/grow; a volume server's
// /status (its disks, and its volumes as they are now). A reclaim's calls
// go over gRPC (masterrpc.go). A bucket of the S3 gateway is a collection
// of the same name; volumes written outside any bucket are the collection
// "".

// seaweed is a SeaweedFS master's client.
type seaweed struct {
	master string
	http   *http.Client
}

// dirStatus is the master's topology (/dir/status).
type dirStatus struct {
	Version  string `json:"Version"`
	Topology struct {
		Max         int `json:"Max"`
		Free        int `json:"Free"`
		DataCenters []struct {
			ID    string `json:"Id"`
			Racks []struct {
				ID        string `json:"Id"`
				DataNodes []struct {
					URL     string `json:"Url"`
					Volumes int    `json:"Volumes"`
					Max     int    `json:"Max"`
				} `json:"DataNodes"`
			} `json:"Racks"`
		} `json:"DataCenters"`
		Layouts []struct {
			Collection string   `json:"collection"`
			Writables  []uint32 `json:"writables"`
		} `json:"Layouts"`
	} `json:"Topology"`
}

// volume is one of the master's volumes (/vol/status), or of a volume
// server's (its /status). CompactRevision counts its compactions.
type volume struct {
	ID               uint32 `json:"Id"`
	Collection       string `json:"Collection"`
	Size             int64  `json:"Size"`
	FileCount        int64  `json:"FileCount"`
	DeleteCount      int64  `json:"DeleteCount"`
	DeletedByteCount int64  `json:"DeletedByteCount"`
	ReadOnly         bool   `json:"ReadOnly"`
	CompactRevision  uint32 `json:"CompactRevision"`
}

// volStatus is every volume, by data center, rack and data node.
type volStatus struct {
	Volumes struct {
		DataCenters map[string]map[string]map[string][]volume `json:"DataCenters"`
	} `json:"Volumes"`
}

// all is every volume, on every node.
func (v volStatus) all() []volume {
	var out []volume
	for _, racks := range v.Volumes.DataCenters {
		for _, nodes := range racks {
			for _, vols := range nodes {
				out = append(out, vols...)
			}
		}
	}
	return out
}

// clusterStatus is the master cluster's leader (/cluster/status).
type clusterStatus struct {
	IsLeader bool   `json:"IsLeader"`
	Leader   string `json:"Leader"`
}

// nodeStatus is a volume server's disks, and its volumes as they are now
// (/status).
type nodeStatus struct {
	DiskStatuses []struct {
		Dir  string `json:"dir"`
		All  int64  `json:"all"`
		Used int64  `json:"used"`
		Free int64  `json:"free"`
	} `json:"DiskStatuses"`
	Volumes []volume `json:"Volumes"`
}

func (s seaweed) dirStatus(ctx context.Context) (dirStatus, error) {
	var out dirStatus
	return out, s.get(ctx, s.master+"/dir/status", &out)
}

func (s seaweed) volStatus(ctx context.Context) (volStatus, error) {
	var out volStatus
	return out, s.get(ctx, s.master+"/vol/status", &out)
}

func (s seaweed) clusterStatus(ctx context.Context) (clusterStatus, error) {
	var out clusterStatus
	return out, s.get(ctx, s.master+"/cluster/status", &out)
}

// nodeStatus reads a data node's disks: node is its address as the master
// has it (host:port).
func (s seaweed) nodeStatus(ctx context.Context, node string) (nodeStatus, error) {
	var out nodeStatus
	return out, s.get(ctx, "http://"+node+"/status", &out)
}

// nodeVolume is one volume as its volume server has it now: the master's
// view of it comes with the server's next heartbeat.
func (s seaweed) nodeVolume(ctx context.Context, node string, id uint32) (volume, error) {
	st, err := s.nodeStatus(ctx, node)
	if err != nil {
		return volume{}, err
	}
	for _, v := range st.Volumes {
		if v.ID == id {
			return v, nil
		}
	}
	return volume{}, fmt.Errorf("volume %d is not on %s", id, node)
}

// grow adds count writable volumes to collection: how many it added.
func (s seaweed) grow(ctx context.Context, collection string, count int) (int, error) {
	q := url.Values{"collection": {collection}, "count": {strconv.Itoa(count)}}
	var out struct {
		Count int `json:"count"`
	}
	if err := s.post(ctx, s.master+"/vol/grow?"+q.Encode(), &out); err != nil {
		return 0, err
	}
	return out.Count, nil
}

func (s seaweed) get(ctx context.Context, u string, into any) error {
	return s.do(ctx, http.MethodGet, u, into)
}

func (s seaweed) post(ctx context.Context, u string, into any) error {
	return s.do(ctx, http.MethodPost, u, into)
}

// do makes a call: an answer that is not 2xx is an error with SeaweedFS's
// message ({"error": ...}), or its status.
func (s seaweed) do(ctx context.Context, method, u string, into any) error {
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			return fmt.Errorf("SeaweedFS: %s", e.Error)
		}
		return fmt.Errorf("SeaweedFS answered %s", resp.Status)
	}
	if into == nil || len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("SeaweedFS's answer does not read: %w", err)
	}
	return nil
}

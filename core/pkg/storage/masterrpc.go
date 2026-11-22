package storage

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/url"
	"strconv"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/mem"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
)

// SeaweedFS's master over gRPC (its master_pb.Seaweed service), for what its
// HTTP API does not do. A reclaim compacts one volume at a time
// (VacuumVolume with its id: the HTTP /vol/vacuum walks every volume, ten
// seconds apart, and answers once all are done), and pauses SeaweedFS's own
// vacuum meanwhile (DisableVacuum, EnableVacuum): the master runs one vacuum
// at a time, and a call that finds one running returns at once, having done
// nothing. Core sends these three, encoded here (weed/pb/master.proto,
// SeaweedFS 4.16: VacuumVolumeRequest is garbage_threshold = 1, a float,
// volume_id = 2, collection = 3; the other two requests, and every answer,
// are empty), rather than take SeaweedFS's module for them.

// masterRPC is the master's gRPC client.
type masterRPC struct {
	addr string
	conn *grpc.ClientConn
}

// masterGRPCAddr is the master's gRPC address: grpcAddr when set, else the
// master URL's host on its port plus 10000 (SeaweedFS's own default: 19333
// for 9333).
func masterGRPCAddr(masterURL, grpcAddr string) (string, error) {
	if grpcAddr != "" {
		return grpcAddr, nil
	}
	u, err := url.Parse(masterURL)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("SeaweedFS's master URL %q names no host", masterURL)
	}
	port := 9333
	if p := u.Port(); p != "" {
		if port, err = strconv.Atoi(p); err != nil {
			return "", fmt.Errorf("SeaweedFS's master URL %q: its port: %w", masterURL, err)
		}
	}
	return net.JoinHostPort(u.Hostname(), strconv.Itoa(port+10000)), nil
}

// newMasterRPC is the client of the master at addr (host:port). It connects
// as its first call goes.
func newMasterRPC(addr string) (*masterRPC, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("SeaweedFS's master's gRPC address %q: %w", addr, err)
	}
	return &masterRPC{addr: addr, conn: conn}, nil
}

func (m *masterRPC) close() { _ = m.conn.Close() }

// vacuumVolume has the master compact one volume if deleted files hold
// threshold of it or more, answering once it is done; at once, having done
// nothing, while another vacuum runs.
func (m *masterRPC) vacuumVolume(ctx context.Context, threshold float64, id uint32, collection string) error {
	var req []byte
	req = protowire.AppendTag(req, 1, protowire.Fixed32Type)
	req = protowire.AppendFixed32(req, math.Float32bits(float32(threshold)))
	req = protowire.AppendTag(req, 2, protowire.VarintType)
	req = protowire.AppendVarint(req, uint64(id))
	if collection != "" {
		req = protowire.AppendTag(req, 3, protowire.BytesType)
		req = protowire.AppendString(req, collection)
	}
	return m.call(ctx, "VacuumVolume", req)
}

// disableVacuum pauses SeaweedFS's own vacuum (it runs every 15 minutes or
// so): one running stops after the volume it is on. enableVacuum resumes it.
func (m *masterRPC) disableVacuum(ctx context.Context) error {
	return m.call(ctx, "DisableVacuum", nil)
}
func (m *masterRPC) enableVacuum(ctx context.Context) error { return m.call(ctx, "EnableVacuum", nil) }

// call makes one call; its error says the master's address, and its answer.
func (m *masterRPC) call(ctx context.Context, method string, req []byte) error {
	var resp []byte
	err := m.conn.Invoke(ctx, "/master_pb.Seaweed/"+method, req, &resp, grpc.ForceCodecV2(rawCodec{}))
	if err == nil {
		return nil
	}
	st := status.Convert(err)
	if st.Code() == codes.Unavailable {
		return fmt.Errorf("SeaweedFS's master does not answer over gRPC at %s: %s", m.addr, st.Message())
	}
	return fmt.Errorf("SeaweedFS's master, over gRPC at %s, %s: %s", m.addr, method, st.Message())
}

// rawCodec sends a request's bytes as they are, and keeps an answer's.
type rawCodec struct{}

var _ encoding.CodecV2 = rawCodec{}

func (rawCodec) Marshal(v any) (mem.BufferSlice, error) {
	b, ok := v.([]byte)
	if !ok {
		return nil, fmt.Errorf("rawCodec: %T is not bytes", v)
	}
	if len(b) == 0 {
		return nil, nil
	}
	return mem.BufferSlice{mem.SliceBuffer(b)}, nil
}

func (rawCodec) Unmarshal(data mem.BufferSlice, v any) error {
	p, ok := v.(*[]byte)
	if !ok {
		return fmt.Errorf("rawCodec: %T is not *[]byte", v)
	}
	*p = data.Materialize()
	return nil
}

// Name is the content-subtype the codec sends: proto, which SeaweedFS's
// server reads.
func (rawCodec) Name() string { return "proto" }

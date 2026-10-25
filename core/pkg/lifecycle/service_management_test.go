package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/logoscore/logos-core/core/pkg/messaging"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-golang-protocol/protocol"
)

// fakeMgmt stands in for modulemgmt.Service.
type fakeMgmt struct {
	cfg       protocol.ManagementConfig
	asked     []string
	heartbeat []struct {
		instance string
		applied  int64
	}
}

func (f *fakeMgmt) ManagementConfigFor(_ context.Context, name, instance string, _ *protocol.ManagementManifest, applied int64) (protocol.ManagementConfig, error) {
	f.asked = append(f.asked, fmt.Sprintf("%s/%s@%d", name, instance, applied))
	return f.cfg, nil
}

func (f *fakeMgmt) HeartbeatApplied(instance string, applied int64) {
	f.heartbeat = append(f.heartbeat, struct {
		instance string
		applied  int64
	}{instance, applied})
}

// rawEnv builds an envelope from literal JSON, so these tests pin the wire
// field names the module SDK sends.
func rawEnv(opType, payload string) messaging.Envelope {
	return messaging.Envelope{Type: opType, Version: "1.0", Payload: json.RawMessage(payload)}
}

const manifestJSON = `{
	"schema_version": "1",
	"properties": [
		{"key": "limit", "type": "int", "default": 10, "min": 1, "apply": "live"},
		{"key": "hosts", "type": "string_list", "default": ["a"], "apply": "restart"}
	],
	"actions": [{"name": "flush", "risk": "low"}],
	"status": [{"key": "queue_depth", "type": "int"}]
}`

func TestHandleRegister_ManifestStoredAndReplyCarriesManagementConfig(t *testing.T) {
	svc, repo, _ := newTestService()
	mgmt := &fakeMgmt{cfg: protocol.ManagementConfig{Revision: 4, Values: map[string]any{"limit": float64(20)}}}
	svc.SetManagement(mgmt)

	out, err := svc.HandleRegister(context.Background(), rawEnv(OpRegister, `{
		"module_type": "channel", "module_name": "http", "instance": "http-1",
		"rpc_queue": "logos.channel.rpc.http-1", "applied_revision": 3,
		"management": `+manifestJSON+`}`))
	if err != nil {
		t.Fatalf("HandleRegister: %v", err)
	}

	row := repo.rows["http-1"]
	if row.Manifest == nil || len(row.Manifest.Properties) != 2 || row.ManifestHash == "" {
		t.Fatalf("manifest not stored: %+v", row)
	}
	if row.AppliedRevision != 3 {
		t.Errorf("applied_revision = %d, want 3", row.AppliedRevision)
	}
	if len(mgmt.asked) != 1 || mgmt.asked[0] != "http/http-1@3" {
		t.Errorf("config asked for %v", mgmt.asked)
	}

	// On the wire: config.management = {revision, values}.
	b, _ := json.Marshal(out)
	var wire struct {
		Config struct {
			Management *protocol.ManagementConfig `json:"management"`
		} `json:"config"`
	}
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	if m := wire.Config.Management; m == nil || m.Revision != 4 || m.Values["limit"] != float64(20) {
		t.Errorf("config.management = %+v (reply %s)", wire.Config.Management, b)
	}
}

func TestHandleRegister_WithoutManifestOmitsManagementAndClearsIt(t *testing.T) {
	svc, repo, _ := newTestService()
	svc.SetManagement(&fakeMgmt{})
	repo.rows["http-1"] = &models.Module{Instance: "http-1", Manifest: &models.ModuleManifest{}, ManifestHash: "old"}

	out, err := svc.HandleRegister(context.Background(), rawEnv(OpRegister,
		`{"module_type": "channel", "module_name": "http", "instance": "http-1", "rpc_queue": "q"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	var wire struct {
		Config map[string]json.RawMessage `json:"config"`
	}
	_ = json.Unmarshal(b, &wire)
	if _, ok := wire.Config["management"]; ok {
		t.Errorf("config.management present without a manifest: %s", b)
	}
	if row := repo.rows["http-1"]; row.Manifest != nil || row.ManifestHash != "" {
		t.Errorf("manifest not cleared: %+v", row)
	}
}

func TestHandleRegister_ManifestWithoutManagementServiceGetsEmptyConfig(t *testing.T) {
	svc, _, _ := newTestService() // no SetManagement
	out, err := svc.HandleRegister(context.Background(), rawEnv(OpRegister,
		`{"module_type": "channel", "module_name": "http", "instance": "http-1", "rpc_queue": "q", "management": `+manifestJSON+`}`))
	if err != nil {
		t.Fatal(err)
	}
	m := out.(registerReply).Config.Management
	if m == nil || m.Revision != 0 || m.Values == nil {
		t.Errorf("management = %+v, want empty revision-0 config", m)
	}
}

func TestHandleRegister_InvalidManifestIsValidationFailed(t *testing.T) {
	svc, repo, _ := newTestService()
	for name, manifest := range map[string]string{
		"bad schema version": `{"schema_version": "9"}`,
		"bad key":            `{"schema_version": "1", "properties": [{"key": "Bad-Key", "type": "int", "apply": "live"}]}`,
		"default off-spec":   `{"schema_version": "1", "properties": [{"key": "n", "type": "int", "default": "x", "apply": "live"}]}`,
		"bad risk":           `{"schema_version": "1", "actions": [{"name": "go", "risk": "medium"}]}`,
	} {
		_, err := svc.HandleRegister(context.Background(), rawEnv(OpRegister,
			`{"module_type": "channel", "module_name": "http", "instance": "http-1", "rpc_queue": "q", "management": `+manifest+`}`))
		assertRPCCode(t, err, messaging.CodeValidationFailed)
		if _, stored := repo.rows["http-1"]; stored {
			t.Errorf("%s: invalid registration was persisted", name)
		}
	}
}

func TestHandleRegister_ConnectionSchema(t *testing.T) {
	svc, repo, _ := newTestService()
	_, err := svc.HandleRegister(context.Background(), rawEnv(OpRegister,
		`{"module_type": "channel", "module_name": "http", "instance": "http-1", "rpc_queue": "q",
		  "connection": {"schema_version": "1", "version": 2, "fields": [{"key": "base_url", "type": "url", "required": true}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	c := repo.rows["http-1"].Connection
	if c == nil || c.Version != 2 || len(c.Fields) != 1 || c.Fields[0].Key != "base_url" {
		t.Fatalf("connection = %+v", c)
	}

	// A register without one clears it.
	if _, err := svc.HandleRegister(context.Background(), rawEnv(OpRegister,
		`{"module_type": "channel", "module_name": "http", "instance": "http-1", "rpc_queue": "q"}`)); err != nil {
		t.Fatal(err)
	}
	if repo.rows["http-1"].Connection != nil {
		t.Error("connection not cleared")
	}

	delete(repo.rows, "http-1")
	_, err = svc.HandleRegister(context.Background(), rawEnv(OpRegister,
		`{"module_type": "channel", "module_name": "http", "instance": "http-1", "rpc_queue": "q",
		  "connection": {"schema_version": "1", "version": 0, "fields": []}}`))
	assertRPCCode(t, err, messaging.CodeValidationFailed)
	if _, stored := repo.rows["http-1"]; stored {
		t.Error("invalid registration was persisted")
	}
}

func TestHandleHeartbeat_StoresAppliedRevisionAndReportsDrift(t *testing.T) {
	svc, repo, _ := newTestService()
	mgmt := &fakeMgmt{}
	svc.SetManagement(mgmt)
	repo.rows["http-1"] = &models.Module{Instance: "http-1", Status: models.ModuleStatusRegistered, RegisteredAt: time.Now()}

	out, err := svc.HandleHeartbeat(context.Background(), rawEnv(OpHeartbeat,
		`{"instance": "http-1", "status": "healthy", "applied_revision": 2, "metrics": {"queue_depth": 7}}`))
	if err != nil {
		t.Fatal(err)
	}
	if reply := out.(heartbeatReply); reply.ConfigChanged {
		t.Error("config_changed must stay false")
	}
	row := repo.rows["http-1"]
	if row.AppliedRevision != 2 || row.LastMetrics["queue_depth"] != float64(7) {
		t.Errorf("row = %+v", row)
	}
	if len(mgmt.heartbeat) != 1 || mgmt.heartbeat[0].instance != "http-1" || mgmt.heartbeat[0].applied != 2 {
		t.Errorf("drift hook calls = %+v", mgmt.heartbeat)
	}

	// A heartbeat without applied_revision (older SDK) neither overwrites the
	// stored revision nor triggers a drift check.
	if _, err := svc.HandleHeartbeat(context.Background(), rawEnv(OpHeartbeat, `{"instance": "http-1", "status": "healthy"}`)); err != nil {
		t.Fatal(err)
	}
	if repo.rows["http-1"].AppliedRevision != 2 || len(mgmt.heartbeat) != 1 {
		t.Errorf("applied = %d, hook calls = %d", repo.rows["http-1"].AppliedRevision, len(mgmt.heartbeat))
	}
}

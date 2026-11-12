package builds

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/blob"
	"github.com/logoscore/logos-core/core/pkg/channelendpoints"
	"github.com/logoscore/logos-core/core/pkg/database"
	"github.com/logoscore/logos-core/core/pkg/environment"
	"github.com/logoscore/logos-core/core/pkg/messaging"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/logoscore/logos-core/core/pkg/repository"
	"github.com/logoscore/logos-golang-protocol/protocol"
	"go.uber.org/zap"
)

// TestIntegrationBuildWithBOB runs one build through the whole dev stack: a
// running core (whose scheduler offers the build and whose RPC server takes the
// progress and result), BOB (logos-bob-factory) compiling its minion for
// linux/amd64, and core's transfer endpoints between BOB and SeaweedFS (the
// badge down, the binary up). The test only queues the build, in a throwaway
// operation, and reads what happened; the minion, core-dev's own platform,
// says what it was built with when run with "describe".
//
// Opt-in through INTEGRATION_BUILDS, and run inside core-dev, where the
// environment and the store's address are the stack's:
//
//	docker exec -w /app -e INTEGRATION_BUILDS=1 logos-core-dev go test ./pkg/builds/ -run IntegrationBuildWithBOB -v
func TestIntegrationBuildWithBOB(t *testing.T) {
	if os.Getenv("INTEGRATION_BUILDS") == "" {
		t.Skip("INTEGRATION_BUILDS not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	e := environment.GetEnvironmentSettings()
	db, err := database.NewDatabase(ctx)
	if err != nil {
		t.Fatal(err)
	}
	registry := repository.NewModuleRegistryRepository(db)
	channels := channelendpoints.NewService(repository.NewChannelEndpointRepository(db), registry)
	s3 := blob.S3Config{Endpoint: e.SeaweedFSS3Endpoint, AccessKey: e.SeaweedFSS3AccessKey, SecretKey: e.SeaweedFSS3SecretKey, Bucket: e.BuildBucket}
	store, err := blob.NewS3Store(ctx, s3)
	if err != nil {
		t.Fatal(err)
	}
	links, err := NewLinks(e.JWTSecretKey)
	if err != nil {
		t.Fatal(err)
	}
	rpc := messaging.NewRPCClient(messaging.Config{Host: e.RabbitMQHost, Port: e.RabbitMQPort, User: e.RabbitMQUser,
		Password: e.RabbitMQPassword, VHost: e.RabbitMQVHost}, zap.NewNop())
	defer func() { _ = rpc.Close() }()
	// Not started: the running core's scheduler is the one that offers it.
	svc := NewService(Repos{
		Files: repository.NewBuildFileRepository(db), Builds: repository.NewBuildRepository(db),
		Minions: repository.NewMinionRepository(db),
	}, registry, channels, rpc, store, links, Config{}, zap.NewNop())

	op := uuid.New()
	t.Cleanup(func() {
		ctx := context.Background()
		if err := svc.DeleteOperationData(ctx, op); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})
	actor := models.ModuleActor{Type: "user", ID: "integration-test", Name: "integration test"}
	badge, err := svc.UploadFile(ctx, op, "badge.png", "image/png", strings.NewReader("\x89PNG not really"), 15, actor)
	if err != nil {
		t.Fatal(err)
	}

	b, err := svc.CreateBuild(ctx, op, BuildInput{
		Description: "integration test build, core's own",
		ModuleName:  "bob", Target: "linux-elf", Platform: "linux/amd64",
		Values: map[string]any{"kill_date": "2027-01-31T00:00:00Z", "punches": []any{"jab", "uppercut"}, "badge": badge.FileID.String()},
		Connections: []ConnectionInput{{ModuleName: "http", Name: "it-edge", Description: "integration note, core's own",
			Settings: map[string]any{"base_url": "https://edge.example.com"}, Values: map[string]any{"path_prefix": "/cdn/assets/"}}},
		Actor: actor,
	})
	if err != nil {
		t.Fatalf("create the build (are BOB and the HTTP channel registered?): %v %v", err, problemsOf(err))
	}
	for !b.Finished() {
		if ctx.Err() != nil {
			t.Fatalf("the build did not finish: %s, pipeline %+v", b.State, Pipeline(b))
		}
		time.Sleep(250 * time.Millisecond)
		if b, err = svc.Build(ctx, b.BuildID.String()); err != nil {
			t.Fatal(err)
		}
	}
	if b.State != models.BuildStateSucceeded {
		t.Fatalf("build %s: %+v\nlog: %s", b.State, b.Error, b.Log)
	}
	t.Logf("built by %s (%s): plan %+v, commands %v", b.Instance, b.FactoryVersion, b.Plan, b.Commands)
	// BOB's plan: the badge was given, so it was checked; the last step is the
	// upload. Every stage succeeded.
	if len(b.Plan) < 3 || b.Plan[0].ID != "badge" || b.Plan[len(b.Plan)-1].ID != "upload" {
		t.Fatalf("plan %+v", b.Plan)
	}
	for _, st := range Pipeline(b) {
		if st.State != StageSucceeded {
			t.Fatalf("stage %+v of %+v", st, Pipeline(b))
		}
	}

	minions, err := svc.Minions(ctx, op)
	if err != nil || len(minions) != 1 || minions[0].BuildID != b.BuildID {
		t.Fatalf("minions %+v: %v", minions, err)
	}
	r, _, err := svc.OpenArtifact(ctx, b.BuildID.String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	raw, _ := io.ReadAll(r)
	bin := filepath.Join(t.TempDir(), b.Artifact.Filename)
	if err := os.WriteFile(bin, raw, 0o755); err != nil {
		t.Fatal(err)
	}
	described, err := exec.CommandContext(ctx, bin, "describe").Output()
	if err != nil {
		t.Fatalf("%s describe: %v", b.Artifact.Filename, err)
	}
	// It prints core's minion config, baked in as it came (numbered CBOR:
	// BOB's punches are field 3, its http path_prefix parameter 2; a file's
	// bytes never go in a config).
	c, err := protocol.ReadMinionConfig(described)
	if err != nil {
		t.Fatalf("its minion config: %v", err)
	}
	punches, _ := protocol.ArgStrings(c.Values, uint64(3))
	prefix := ""
	if len(c.Connections) == 1 {
		prefix, _ = protocol.ArgString(c.Connections[0].Values, uint64(2))
	}
	if string(c.Build) != string(b.BuildID[:]) || len(punches) != 2 || prefix != "/cdn/assets/" {
		t.Fatalf("its minion config: %+v", c)
	}
	// Its exit codes are its own: drawn by core for the three BOB does not
	// fix, built in by BOB, which names them in the build's log.
	drawn := b.DrawnErrorCodes()
	if !b.UniqueErrorCodes || len(drawn) != 3 {
		t.Fatalf("drawn %+v", b.ErrorCodes)
	}
	parts := make([]string, len(drawn))
	for i, d := range drawn {
		parts[i] = fmt.Sprintf("%d→%d", d.Code, d.ExitCode)
	}
	if want := "exit codes of its own: " + strings.Join(parts, ", "); !strings.Contains(b.Log, want) {
		t.Fatalf("BOB's log lacks %q:\n%s", want, b.Log)
	}
	// The descriptions stayed in core.
	if strings.Contains(string(raw), "integration note") || b.ConnectionDescriptions[0] != "integration note, core's own" ||
		strings.Contains(string(raw), "integration test build") || b.Description != "integration test build, core's own" {
		t.Fatalf("descriptions %q %q", b.Description, b.ConnectionDescriptions)
	}
	t.Logf("artifact %s (%d bytes), a %d-byte minion config, %s", b.Artifact.Filename, b.Artifact.Size, len(described), strings.Join(parts, ", "))
}

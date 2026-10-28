package builds

import (
	"context"
	"encoding/json"
	"io"
	"os"
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
	"go.uber.org/zap"
)

// TestIntegrationBuildWithBOB runs one build through the whole dev stack: a
// running core (whose scheduler offers the build and whose RPC server takes the
// progress and result), BOB (logos-bob-factory) building it, and SeaweedFS in
// between. The test only queues the build, in a throwaway operation, and reads
// what happened.
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
	signer, err := blob.NewPresigner(s3)
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
	}, registry, channels, rpc, store, signer, Config{}, zap.NewNop())

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
		ModuleName:  "bob", Target: "dummy", Platform: "any/any",
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
		if b, err = svc.Build(ctx, b.MinionID.String()); err != nil {
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
	if err != nil || len(minions) != 1 || minions[0].MinionID != b.MinionID {
		t.Fatalf("minions %+v: %v", minions, err)
	}
	r, _, err := svc.OpenArtifact(ctx, b.MinionID.String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	raw, _ := io.ReadAll(r)
	var minion struct {
		MinionID    string                  `json:"minion_id"`
		Punches     []string                `json:"punches"`
		Badge       struct{ SHA256 string } `json:"badge"`
		Connections []struct {
			Values map[string]any `json:"values"`
		} `json:"connections"`
	}
	if err := json.Unmarshal(raw, &minion); err != nil {
		t.Fatalf("artifact %s: %v", raw, err)
	}
	if minion.MinionID != b.MinionID.String() || minion.Badge.SHA256 != badge.SHA256 || len(minion.Punches) != 2 ||
		minion.Connections[0].Values["path_prefix"] != "/cdn/assets/" {
		t.Fatalf("artifact %s", raw)
	}
	// The descriptions stayed in core.
	if strings.Contains(string(raw), "integration note") || b.ConnectionDescriptions[0] != "integration note, core's own" ||
		strings.Contains(string(raw), "integration test build") || b.Description != "integration test build, core's own" {
		t.Fatalf("descriptions %q %q, artifact %s", b.Description, b.ConnectionDescriptions, raw)
	}
	t.Logf("artifact %s (%d bytes):\n%s", b.Artifact.Filename, b.Artifact.Size, raw)
}

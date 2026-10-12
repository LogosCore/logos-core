package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/cache"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/middleware"
	"github.com/logoscore/logos-core/core/pkg/models"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

func stageServer() (*Server, *memCache) {
	c := newMemCache()
	return &Server{deps: Deps{Cache: c, Logger: zap.NewNop()}}, c
}

// stageChunks stages text in the given pieces the way an agent would: the
// first without a stage_id, each later one at the offset the last returned.
func stageChunks(t *testing.T, s *Server, ctx context.Context, chunks ...string) string {
	t.Helper()
	var id string
	var offset int64
	for _, chunk := range chunks {
		res, err := handleStageText(ctx, s, stageTextArgs{StageID: id, Offset: offset, Text: chunk})
		if err != nil {
			t.Fatalf("staging a chunk failed: %v", err)
		}
		view := res.Payload.(stageView)
		id, offset = view.StageID, view.Bytes
	}
	return id
}

// The point of the feature: text sent in pieces comes out whole, and a write
// tool reads it exactly as if it had been inline.
func TestStaging_ChunksComeOutWhole(t *testing.T) {
	s, _ := stageServer()
	ctx := agentCtxNamed("key-1")

	id := stageChunks(t, s, ctx, "USER  PID ", "привет ", "end\n")

	got, err := s.stagedText(ctx, "", id, "answer")
	if err != nil {
		t.Fatal(err)
	}
	if got != "USER  PID привет end\n" {
		t.Fatalf("staged text came out as %q", got)
	}
}

// A wrong offset means a chunk was lost or reordered. Appending anyway would
// write a corrupted answer, so it is refused and the refusal says where to
// resume.
func TestStaging_WrongOffsetIsRefusedWithTheResumePoint(t *testing.T) {
	s, _ := stageServer()
	ctx := agentCtxNamed("key-1")
	id := stageChunks(t, s, ctx, "0123456789")

	_, err := handleStageText(ctx, s, stageTextArgs{StageID: id, Offset: 4, Text: "xyz"})
	if err == nil || !isRefusal(err) {
		t.Fatalf("a chunk at the wrong offset was accepted: %v", err)
	}
	if !strings.Contains(err.Error(), "offset 10") {
		t.Fatalf("the refusal does not say where to resume: %v", err)
	}
}

// Agents retry when a reply is lost. Resending the chunk that already landed
// must not duplicate it.
func TestStaging_ResentChunkIsNotDuplicated(t *testing.T) {
	s, _ := stageServer()
	ctx := agentCtxNamed("key-1")
	id := stageChunks(t, s, ctx, "first ", "second")

	res, err := handleStageText(ctx, s, stageTextArgs{StageID: id, Offset: 6, Text: "second"})
	if err != nil {
		t.Fatalf("a resent chunk was refused: %v", err)
	}
	if n := res.Payload.(stageView).Bytes; n != 12 {
		t.Fatalf("a resent chunk changed the length to %d", n)
	}
	if got, _ := s.stagedText(ctx, "", id, "answer"); got != "first second" {
		t.Fatalf("a resent chunk was appended twice: %q", got)
	}
}

// Stage ids are namespaced per agent key: one agent cannot read, or write
// into a page, what another staged.
func TestStaging_StagesArePrivateToTheAgentKey(t *testing.T) {
	s, _ := stageServer()
	id := stageChunks(t, s, agentCtxNamed("key-1"), "secret")

	if _, err := s.stagedText(agentCtxNamed("key-2"), "", id, "answer"); err == nil {
		t.Fatal("another agent key read a stage it did not create")
	}
}

func TestStaging_InlineAndStageTogetherIsRefused(t *testing.T) {
	s, _ := stageServer()
	ctx := agentCtxNamed("key-1")
	id := stageChunks(t, s, ctx, "staged")

	_, err := s.stagedText(ctx, "inline", id, "answer")
	if err == nil || !strings.Contains(err.Error(), "answer or stage_id") {
		t.Fatalf("both inline text and a stage were accepted: %v", err)
	}
}

func TestStaging_UnknownStageIsRefused(t *testing.T) {
	s, _ := stageServer()
	ctx := agentCtxNamed("key-1")

	_, err := s.stagedText(ctx, "", uuid.NewString(), "answer")
	if err == nil || !isRefusal(err) {
		t.Fatalf("an unknown stage resolved: %v", err)
	}
}

// Staged text has no use except a write, so a read-only key cannot stage.
func TestStaging_ReadOnlyKeyIsRefused(t *testing.T) {
	s, _ := stageServer()
	ctx := gqlctx.WithAuthInfo(context.Background(), gqlctx.AuthInfo{
		Agent: &gqlctx.AgentInfo{AgentKeyID: "ro", AllowWrites: false},
	})
	if _, err := handleStageText(ctx, s, stageTextArgs{Text: "x"}); err == nil {
		t.Fatal("a read-only key staged text")
	}
}

// Without Redis there is nowhere to keep chunks; say so plainly rather than
// accept text that would then be lost.
func TestStaging_UnavailableWithoutACache(t *testing.T) {
	s := &Server{deps: Deps{Cache: cache.NewNoopCache(), Logger: zap.NewNop()}}
	_, err := handleStageText(agentCtxNamed("key-1"), s, stageTextArgs{Text: "x"})
	if !errors.Is(err, errStagingUnavailable) {
		t.Fatalf("staging without a cache did not say it is unavailable: %v", err)
	}
}

func TestStaging_CapIsEnforced(t *testing.T) {
	s, c := stageServer()
	ctx := agentCtxNamed("key-1")
	id := uuid.NewString()
	c.data[stageKey("key-1", id)] = strings.Repeat("a", maxStageBytes)

	_, err := handleStageText(ctx, s, stageTextArgs{StageID: id, Offset: maxStageBytes, Text: "b"})
	if err == nil || !strings.Contains(err.Error(), "capped") {
		t.Fatalf("a chunk past the cap was accepted: %v", err)
	}
}

// Batch entries take a stage_id of their own, so several large answers can
// go in one call.
func TestStaging_ChecklistBatchEntriesResolveTheirStages(t *testing.T) {
	s, _ := stageServer()
	ctx := agentCtxNamed("key-1")
	id := stageChunks(t, s, ctx, "```\n", "ps auxf output\n", "```")

	args := setChecklistAnswerArgs{Answers: []checklistAnswerEntry{
		{Key: "k1", Answer: "inline"},
		{Key: "k2", StageRef: StageRef{StageID: id}},
	}}
	answers, err := args.mergedAnswers(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if answers[1].Markdown != "```\nps auxf output\n```" {
		t.Fatalf("the staged entry resolved to %q", answers[1].Markdown)
	}
	if ids := args.stageIDs(); len(ids) != 1 || ids[0] != id {
		t.Fatalf("stageIDs missed the batch entry's stage: %v", ids)
	}
}

// A stage is used up by the write that succeeds with it, and survives a
// write that fails, so the agent can fix the problem and retry without
// staging everything again.
func TestStaging_ReleasedOnlyAfterSuccess(t *testing.T) {
	s, c := stageServer()
	ctx := agentCtxNamed("key-1")
	id := stageChunks(t, s, ctx, "evidence")
	args := setChecklistAnswerArgs{Key: "k1", StageRef: StageRef{StageID: id}}

	failing := func(context.Context, *Server, setChecklistAnswerArgs) (toolResult, error) {
		return toolResult{}, refuse("nope")
	}
	if _, err := invoke(ctx, s, "set_checklist_answer", writeTool, args, failing); err == nil {
		t.Fatal("the failing handler did not fail")
	}
	if _, ok := c.data[stageKey("key-1", id)]; !ok {
		t.Fatal("a failed write released the stage; the agent would have to stage it all again")
	}

	ok := func(context.Context, *Server, setChecklistAnswerArgs) (toolResult, error) {
		return toolResult{}, nil
	}
	if _, err := invoke(ctx, s, "set_checklist_answer", writeTool, args, ok); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.data[stageKey("key-1", id)]; ok {
		t.Fatal("a successful write left its stage behind")
	}
}

// The audit row records the shape of a chunk, never the chunk itself.
func TestStaging_AuditLeavesTheTextOut(t *testing.T) {
	encoded := encodeArgs(auditForm(stageTextArgs{StageID: "s", Offset: 3, Text: "sensitive output"}))
	if strings.Contains(encoded, "sensitive") {
		t.Fatalf("the audit row carries the staged text: %s", encoded)
	}
	if !strings.Contains(encoded, `"size_bytes":16`) {
		t.Fatalf("the audit row does not record the chunk size: %s", encoded)
	}
}

func stageRouter(t *testing.T, s *Server, key *models.AgentKey) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userID", uuid.New().String())
		if key != nil {
			c.Set(middleware.AgentAuthFlag, true)
			c.Set(middleware.AgentInfoKey, key)
		}
		c.Next()
	})
	r.POST("/mcp/stage", s.StageHandler())
	return r
}

// The shell path: a raw body becomes a stage in one request, and the query
// parameters continue it exactly as the tool's arguments do.
func TestStageHandler_StagesARawBody(t *testing.T) {
	s, _ := stageServer()
	s.server = mcp.NewServer(&mcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	key := &models.AgentKey{AgentKeyID: uuid.New(), MaxRole: models.OperationRoleOperator, AllowWrites: true}
	r := stageRouter(t, s, key)

	post := func(query, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/mcp/stage"+query, strings.NewReader(body))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	w := post("", "line one\n")
	if w.Code != http.StatusCreated {
		t.Fatalf("staging a raw body: %d %s", w.Code, w.Body.String())
	}
	var view stageView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}

	w = post("?stage_id="+view.StageID+"&offset=9", "line two\n")
	if w.Code != http.StatusCreated {
		t.Fatalf("continuing a stage: %d %s", w.Code, w.Body.String())
	}

	ctx := agentCtxNamed(key.AgentKeyID.String())
	if got, _ := s.stagedText(ctx, "", view.StageID, "content"); got != "line one\nline two\n" {
		t.Fatalf("the shell-staged text came out as %q", got)
	}

	if w := post("?stage_id="+view.StageID+"&offset=1", "x"); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a wrong offset over HTTP was not refused: %d", w.Code)
	}
}

func TestStageHandler_RefusesNonAgentCallers(t *testing.T) {
	s, _ := stageServer()
	req := httptest.NewRequest(http.MethodPost, "/mcp/stage", strings.NewReader("x"))
	w := httptest.NewRecorder()
	stageRouter(t, s, nil).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("a non-agent caller staged text: %d", w.Code)
	}
}

// A long text attachment reads in windows that chain to the whole file:
// before, only its first 200 KB could ever be seen.
func TestReadAttachment_PagesThroughLongText(t *testing.T) {
	s, _ := stageServer()
	file := &models.WikiFile{FileID: uuid.New(), Filename: "ps.txt"}
	full := strings.Repeat("process line ж\n", (2*maxAttachmentTextBytes)/16+100)

	var got strings.Builder
	offset := 0
	for range 10 {
		res, err := s.textAttachmentResult(file, full, "text", false, offset)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(res.Payload)
		var page struct {
			Text       string `json:"text"`
			NextOffset int    `json:"nextOffset"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatal(err)
		}
		got.WriteString(page.Text)
		if page.NextOffset == 0 {
			break
		}
		offset = page.NextOffset
	}
	if got.String() != full {
		t.Fatalf("paging read %d of %d bytes, or not in order", got.Len(), len(full))
	}
}

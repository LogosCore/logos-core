package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/logoscore/logos-core/core/pkg/cache"
	"github.com/logoscore/logos-core/core/pkg/graphql/gqlctx"
	"github.com/logoscore/logos-core/core/pkg/middleware"
	"github.com/logoscore/logos-core/core/pkg/responses"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Staging lets an agent hand over text too large to write in one tool call.
//
// Every write tool takes its content inline, as an argument the model has to
// emit whole. That is fine for a paragraph and fails for the full output of a
// command: a production agent could not put 114 KB of `ps auxf` into a
// checklist answer, because it could neither read the file in one go nor
// produce the argument in one turn, and nothing let it send the text in parts.
// The server would have accepted it; the agent had no way to deliver it.
//
// So text can be staged first, in chunks (stage_text) or straight from a
// shell (POST /api/v1/mcp/stage), and any content-taking tool accepts the
// returned stage_id in place of its inline field. The tool then runs exactly
// as if the text had been inline: same validation, same caps.

const (
	// stageTTL is how long staged text waits to be used. Refreshed by every
	// chunk, so it bounds the gap between calls, not the whole upload.
	stageTTL = time.Hour
	// maxStageBytes matches the largest consumer, attachments. Wiki writes
	// cap lower (1 MB) and refuse on their own, as they do for inline text.
	maxStageBytes = maxAttachmentBytes
	// stageToolName is the tool, and what the HTTP form is audited as.
	stageToolName = "stage_text"
)

type stageTextArgs struct {
	StageID string `json:"stage_id,omitempty" jsonschema:"Continue this stage; omit on the first chunk."`
	Offset  int64  `json:"offset,omitempty"   jsonschema:"The bytes value the previous chunk returned; 0 for the first."`
	Text    string `json:"text"               jsonschema:"The next chunk, in order."`
}

// auditArgs keeps the chunk itself out of the audit row: a long upload would
// otherwise write its whole body into the trail, a slice per call.
func (a stageTextArgs) auditArgs() any {
	return stageAuditArgs{StageID: a.StageID, Offset: a.Offset, SizeBytes: int64(len(a.Text))}
}

type stageAuditArgs struct {
	StageID   string `json:"stage_id,omitempty"`
	Offset    int64  `json:"offset"`
	SizeBytes int64  `json:"size_bytes"`
}

type stageView struct {
	StageID string   `json:"stage_id"`
	Bytes   int64    `json:"bytes"`
	Notes   []string `json:"notes,omitempty"`
}

// StageRef is embedded by the args of every tool whose content can come from
// a stage. One field, one description, the same on every tool.
type StageRef struct {
	StageID string `json:"stage_id,omitempty" jsonschema:"Text from stage_text, in place of the inline content."`
}

func (r StageRef) stageIDs() []string { return nonEmpty(r.StageID) }

// stagedArgs is implemented by tool args that can reference stages. invoke
// releases them once the tool succeeds, so no handler has to remember to.
type stagedArgs interface {
	stageIDs() []string
}

func registerStagingTools(s *Server) {
	register(s, &mcp.Tool{
		Name: stageToolName,
		Description: "Stage text too large for one call, in chunks; pass the stage_id to the write tool. " +
			"See wiki.md.",
	}, readTool, handleStageText)
}

// handleStageText appends one chunk.
//
// Registered as a read so a long upload does not spend the tight write
// budget, but it refuses read-only keys itself: staged text has no use
// except a write.
func handleStageText(ctx context.Context, s *Server, args stageTextArgs) (toolResult, error) {
	if err := requireWrites(ctx); err != nil {
		return toolResult{}, err
	}
	if args.Text == "" {
		return toolResult{}, refuse("text is empty; send the next chunk.")
	}
	return s.appendStage(ctx, args.StageID, args.Offset, args.Text)
}

func (s *Server) appendStage(ctx context.Context, stageID string, offset int64, text string) (toolResult, error) {
	if s.deps.Cache == nil {
		return toolResult{}, errStagingUnavailable
	}
	agentKeyID := agentKeyIDFromContext(ctx)

	if stageID == "" {
		if offset != 0 {
			return toolResult{}, refuse("offset %d without a stage_id: the first chunk starts a stage at 0.", offset)
		}
		stageID = uuid.NewString()
	} else if _, err := uuid.Parse(stageID); err != nil {
		return toolResult{}, refuse("stage_id %q is not one stage_text returned.", stageID)
	}
	if offset < 0 {
		return toolResult{}, refuse("offset cannot be negative.")
	}

	n, err := s.deps.Cache.AppendAt(ctx, stageKey(agentKeyID, stageID), offset, text, maxStageBytes, stageTTL)
	if err != nil {
		var mismatch *cache.OffsetMismatchError
		switch {
		case errors.Is(err, cache.ErrCacheDisabled):
			return toolResult{}, errStagingUnavailable
		case errors.Is(err, cache.ErrTooLarge):
			return toolResult{}, refuse("staged text is capped at %d MB; this chunk would pass it.",
				maxStageBytes/(1024*1024))
		case errors.As(err, &mismatch) && mismatch.Have == 0:
			return toolResult{}, refuse("stage %s is empty or expired (stages last %s after the last chunk). "+
				"Start again without stage_id.", stageID, stageTTL)
		case errors.As(err, &mismatch):
			return toolResult{}, refuse("stage %s holds %d bytes, not %d. Resend from offset %d.",
				stageID, mismatch.Have, offset, mismatch.Have)
		}
		return toolResult{}, fmt.Errorf("failed to stage text: %w", err)
	}

	return toolResult{
		Payload: stageView{
			StageID: stageID,
			Bytes:   n,
			Notes: []string{fmt.Sprintf(
				"Next chunk: offset:%d. When done, pass stage_id to the write tool instead of its content.", n)},
		},
		Summary: fmt.Sprintf("staged %d bytes", n),
	}, nil
}

var errStagingUnavailable = refuse("staging is unavailable on this server; send the content inline in one call.")

// stagedText returns what a content field resolves to: the inline text, or
// the staged text when stageID is set. field names the inline argument in
// the refusal when both are given.
func (s *Server) stagedText(ctx context.Context, inline, stageID, field string) (string, error) {
	if stageID == "" {
		return inline, nil
	}
	if inline != "" {
		return "", refuse("give %s or stage_id, not both.", field)
	}
	if s.deps.Cache == nil {
		return "", errStagingUnavailable
	}
	text, err := s.deps.Cache.Get(ctx, stageKey(agentKeyIDFromContext(ctx), stageID))
	if errors.Is(err, cache.ErrCacheDisabled) {
		return "", errStagingUnavailable
	}
	// Missing, expired, or another agent's: all read as absent, and the key
	// namespace means another agent's stage cannot be reached at all.
	if err != nil || text == "" {
		return "", refuse("stage %s was not found: it expired (%s after its last chunk), was already "+
			"used by a successful write, or was never filled. Stage the text again.", stageID, stageTTL)
	}
	if !utf8.ValidString(text) {
		return "", refuse("stage %s is not valid UTF-8 text. Chunks must be sent in order and whole.", stageID)
	}
	return text, nil
}

// releaseStages drops stages a successful write has consumed. Best-effort:
// one left behind expires on its own, and the write already happened.
func (s *Server) releaseStages(ctx context.Context, args any) {
	staged, ok := args.(stagedArgs)
	if !ok || s.deps.Cache == nil {
		return
	}
	ids := staged.stageIDs()
	if len(ids) == 0 {
		return
	}
	agentKeyID := agentKeyIDFromContext(ctx)
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = stageKey(agentKeyID, id)
	}
	if err := s.deps.Cache.Del(ctx, keys...); err != nil {
		s.deps.Logger.Debug("mcp: could not release staged text")
	}
}

// stageKey namespaces by agent key, so a stage id is only meaningful to the
// agent that created it.
func stageKey(agentKeyID, stageID string) string {
	return "mcp:stage:" + agentKeyID + ":" + stageID
}

func nonEmpty(ids ...string) []string {
	var out []string
	for _, id := range ids {
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

// StageHandler is POST /api/v1/mcp/stage: stage_text for agents with a shell.
//
// The body is the raw text — `curl --data-binary @out.txt` — so a command's
// output reaches the server without ever passing through the model, which is
// the only way that scales to anything large. Query parameters stage_id and
// offset continue a stage exactly as the tool's arguments do. Same agent-only
// gate, rate limit and audit as the tool, via invoke and record.
func (s *Server) StageHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !middleware.HasAgentAuth(c) {
			c.AbortWithStatusJSON(http.StatusForbidden, responses.ErrForbidden)
			return
		}
		auth, err := authInfoFromGin(c)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusForbidden, responses.ErrForbidden)
			return
		}
		ctx := gqlctx.WithAuthInfo(c.Request.Context(), auth)

		var offset int64
		if raw := c.Query("offset"); raw != "" {
			offset, err = strconv.ParseInt(raw, 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, responses.NewErrorResponse("offset must be a whole number of bytes"))
				return
			}
		}
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxStageBytes))
		if err != nil {
			c.JSON(http.StatusRequestEntityTooLarge,
				responses.NewErrorResponse("staged text is capped at %d MB", maxStageBytes/(1024*1024)))
			return
		}

		args := stageTextArgs{StageID: c.Query("stage_id"), Offset: offset, Text: string(body)}

		started := time.Now()
		result, err := invoke(ctx, s, stageToolName, readTool, args, handleStageText)
		s.record(ctx, auditEntry{
			tool:     stageToolName,
			kind:     readTool,
			args:     args,
			result:   result,
			err:      err,
			duration: time.Since(started),
		})

		if err != nil {
			status := http.StatusInternalServerError
			if isRefusal(err) {
				status = http.StatusUnprocessableEntity
			}
			c.JSON(status, responses.NewErrorResponse("%s", err.Error()))
			return
		}
		c.JSON(http.StatusCreated, result.Payload)
	}
}

package mcp

import (
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server holds the single MCP server instance and everything its tools need.
//
// One server serves every agent. Per-request identity travels on the context
// rather than in the server, which is why the tools take ctx and why
// transport_test.go pins that the context actually arrives — a shared server
// whose handlers silently lost the caller's identity would be an authorization
// hole, not a bug.
type Server struct {
	server *mcp.Server
	deps   Deps
	limits rateLimits
	// tools is the registry the generated skill is built from. Appended to by
	// register during New; never mutated afterwards.
	tools []toolDoc

	// audit carries recorded calls to the worker that writes them — see
	// dispatch.go. Closed by Close.
	audit     chan auditJob
	auditDone sync.WaitGroup
	closeOnce sync.Once
}

// toolDoc is one tool as the skill describes it.
type toolDoc struct {
	Name        string
	Description string
	Write       bool
}

// New builds the MCP server and registers the tool surface.
func New(deps Deps) *Server {
	s := &Server{
		server: mcp.NewServer(&mcp.Implementation{
			Name:    serverName,
			Version: serverVersion,
		}, &mcp.ServerOptions{
			Instructions: serverInstructions,
		}),
		deps:   deps,
		limits: rateLimits{calls: deps.CallsPerMinute, writes: deps.WritesPerMinute},
		audit:  make(chan auditJob, auditQueueSize),
	}
	s.auditDone.Add(1)
	go s.runAuditWorker()

	registerOperationTools(s)
	registerHostTools(s)
	registerCredentialTools(s)
	registerHashTools(s)
	registerTaskTools(s)
	registerWikiTools(s)
	registerWikiMetadataTools(s)
	registerWikiDrawingTools(s)
	registerTimelineTools(s)
	registerAttachmentTools(s)
	registerFocusTools(s)
	registerSkillTools(s)

	registerResources(s)
	registerPrompts(s)

	return s
}

// serverInstructions is the standing brief every connecting client receives.
// It exists because the difference between an agent that helps and one that
// gets in the way is mostly knowing what surface it is on and what it must not
// assume.
// Every client receives this on connect and carries it for the whole session,
// so it is charged per turn and has to stay short. The last line is what makes
// that affordable: it buys the full guide on demand, in any client, instead of
// paying for it permanently here.
const serverInstructions = `Logos is a shared workspace where a team keeps track of an operation:
the hosts involved, credentials, hashes, tasks, wiki pages and a timeline. You act for one
operator and inherit their permissions.

Your tools read and write those records, and that is all — nothing here runs commands or reaches
any system outside Logos.

Every call, reads included, is recorded and shown to the operator. Tools default to the operation
the operator has open (get_user_focus shows it). Results are capped: truncated means narrow the
filter. Arguments hold a megabyte, so never split a value across calls. Prefer edit_wiki_document
and add_wiki_section over update_wiki_document.

New here? Read the resource logos://guide first; it lists the per-job guides.`

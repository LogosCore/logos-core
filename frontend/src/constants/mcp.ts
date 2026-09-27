// The endpoint an agent's MCP client points at. Named here rather than written
// into the copyable snippet so the connect instructions and the skill download
// (which lives under the same path) cannot drift apart.
export const MCP_ENDPOINT_PATH = "/api/v1/mcp"

/**
 * The absolute endpoint for this deployment, for the config an operator copies
 * into their MCP client. Absolute rather than a placeholder host: the snippet is
 * meant to be pasted and work, and a `$HOST` the operator has to substitute is
 * a step where they can get it wrong.
 */
export function mcpEndpointUrl(): string {
  return `${window.location.origin}${MCP_ENDPOINT_PATH}`
}

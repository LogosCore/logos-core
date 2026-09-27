import { MCP_ENDPOINT_PATH } from "@/constants/mcp"

// The one place the skill download endpoint is named. Shared by the agents
// page download button and the update prompt so they cannot drift. Derived from
// the MCP endpoint because the server serves it from under that path.
export const SKILL_DOWNLOAD_URL = `${MCP_ENDPOINT_PATH}/skill`

import { graphql } from "@/graphql/gql"

// An operation's VPN tunnels (core: vpn_tunnels.graphql, docs:
// contracts/vpn-front): their routes and machines while their tasks run.
// The list a page at a time, kept live by the operation's VPN events:
// opened, their clients or streams' numbers, closed.
export const VPNTunnelFields = graphql(`
  fragment VPNTunnelFields on VPNTunnel {
    id
    routes
    sessionId
    sessionNumber
    taskId
    openedBy
    openedByName
    openedAt
    listening
    clients
    open
    streams
    stopsAt
  }
`)

export const VPNTunnelsQuery = graphql(`
  query VPNTunnels($operationId: ID!, $search: String, $first: Int, $after: String) {
    vpnTunnels(operationId: $operationId, search: $search, first: $first, after: $after) {
      edges {
        node {
          ...VPNTunnelFields
        }
        cursor
      }
      pageInfo {
        hasNextPage
        endCursor
      }
      totalCount
    }
  }
`)

export const VPNTunnelChangedSubscription = graphql(`
  subscription VPNTunnelChanged($operationId: ID!) {
    vpnTunnelChanged(operationId: $operationId) {
      action
      id
      operationId
      tunnel {
        ...VPNTunnelFields
      }
    }
  }
`)

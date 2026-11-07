import { graphql } from "@/graphql/gql"

// An operation's operational ports in use: tunnel tasks' SOCKS proxies
// (core: operational_ports.graphql). The list a page at a time, kept live
// by the operation's port events: opened, their streams' numbers, closed.
export const OperationalPortFields = graphql(`
  fragment OperationalPortFields on OperationalPort {
    id
    port
    purpose
    to
    address
    sessionId
    sessionNumber
    taskId
    openedBy
    openedByName
    openedAt
    listening
    open
    streams
    stopsAt
  }
`)

export const OperationalPortsQuery = graphql(`
  query OperationalPorts($operationId: ID!, $search: String, $first: Int, $after: String) {
    operationalPorts(operationId: $operationId, search: $search, first: $first, after: $after) {
      edges {
        node {
          ...OperationalPortFields
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

export const OperationalPortChangedSubscription = graphql(`
  subscription OperationalPortChanged($operationId: ID!) {
    operationalPortChanged(operationId: $operationId) {
      action
      id
      operationId
      port {
        ...OperationalPortFields
      }
    }
  }
`)

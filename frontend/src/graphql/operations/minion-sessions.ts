import { graphql } from "@/graphql/gql"

// A minion session as the Sessions table shows it: read-only, opened by a
// minion's hello.
export const MinionSessionFields = graphql(`
  fragment MinionSessionFields on MinionSession {
    id
    operationId
    buildId
    buildDescription
    moduleName
    target
    platform
    staged
    pid
    os
    arch
    host
    user
    domain
    proc
    integrity
    addrs
    version
    x
    createdAt
    lastSeenAt
  }
`)

export const MinionSessionsQuery = graphql(`
  query MinionSessions($operationId: ID!, $search: String, $first: Int, $after: String) {
    minionSessions(operationId: $operationId, search: $search, first: $first, after: $after) {
      edges {
        node {
          ...MinionSessionFields
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

export const MinionSessionChangedSubscription = graphql(`
  subscription MinionSessionChanged($operationId: ID!) {
    minionSessionChanged(operationId: $operationId) {
      action
      sessionId
      operationId
      session {
        ...MinionSessionFields
      }
    }
  }
`)

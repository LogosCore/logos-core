import { graphql } from "@/graphql/gql"

// A minion session as the Sessions table shows it: read-only, opened by a
// minion's hello.
export const MinionSessionFields = graphql(`
  fragment MinionSessionFields on MinionSession {
    id
    state
    archived
    number
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
    address
    addressPicked
    sleepInterval
    sleepJitter
    lateAt
    lostAt
    persisted
    restartCount
    restarts {
      at
      pid
      host
      buildId
    }
    peerAddress
    peers {
      address
      firstSeen
      lastSeen
      count
    }
    version
    x
    createdAt
    lastSeenAt
  }
`)

export const MinionSessionsQuery = graphql(`
  query MinionSessions(
    $operationId: ID!
    $archived: Boolean
    $search: String
    $filters: [MinionSessionFilterInput!]
    $sort: MinionSessionSortInput
    $first: Int
    $after: String
  ) {
    minionSessions(
      operationId: $operationId
      archived: $archived
      search: $search
      filters: $filters
      sort: $sort
      first: $first
      after: $after
    ) {
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

export const MinionSessionValuesQuery = graphql(`
  query MinionSessionValues($operationId: ID!, $column: String!) {
    minionSessionValues(operationId: $operationId, column: $column)
  }
`)

export const MinionSessionsLayoutFields = graphql(`
  fragment MinionSessionsLayoutFields on MinionSessionsLayout {
    columns
    sort {
      column
      direction
    }
    filters {
      column
      op
      text
      values
      min
      max
      seconds
      from
      to
    }
    split
  }
`)

export const MinionSessionsLayoutQuery = graphql(`
  query MinionSessionsLayout {
    minionSessionsLayout {
      ...MinionSessionsLayoutFields
    }
  }
`)

export const SetMinionSessionsLayoutMutation = graphql(`
  mutation SetMinionSessionsLayout($layout: MinionSessionsLayoutInput) {
    setMinionSessionsLayout(layout: $layout) {
      ...MinionSessionsLayoutFields
    }
  }
`)

export const SetMinionSessionAddressMutation = graphql(`
  mutation SetMinionSessionAddress($id: ID!, $address: String) {
    setMinionSessionAddress(id: $id, address: $address) {
      ...MinionSessionFields
    }
  }
`)

export const MinionSessionQuery = graphql(`
  query MinionSession($id: ID!) {
    minionSession(id: $id) {
      ...MinionSessionFields
    }
  }
`)

export const SetMinionSessionArchivedMutation = graphql(`
  mutation SetMinionSessionArchived($id: ID!, $archived: Boolean!) {
    setMinionSessionArchived(id: $id, archived: $archived) {
      ...MinionSessionFields
    }
  }
`)

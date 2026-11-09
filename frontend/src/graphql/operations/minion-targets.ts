import { graphql } from "@/graphql/gql"

export const MinionTargetFields = graphql(`
  fragment MinionTargetFields on MinionTarget {
    id
    operationId
    buildId
    buildDescription
    conn
    connName
    moduleName
    kind
    values
    name
    retries
    policy
    state
    stateDetail
    stateAt
    problems {
      key
      message
    }
    sessionId
    session {
      id
      number
    }
    delegateId
    delegate {
      id
      number
      host
      user
    }
    failures
    unreachableSince
    nextCallAt
    lastCallAt
    lastOkAt
    calls
    createdBy {
      type
      id
      name
    }
    createdAt
    updatedAt
  }
`)

export const MinionTargetsQuery = graphql(`
  query MinionTargets(
    $operationId: ID!
    $search: String
    $buildId: ID
    $states: [String!]
    $first: Int
    $after: String
  ) {
    minionTargets(
      operationId: $operationId
      search: $search
      buildId: $buildId
      states: $states
      first: $first
      after: $after
    ) {
      edges {
        node {
          ...MinionTargetFields
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

export const BuildBindConnectionsQuery = graphql(`
  query BuildBindConnections($id: ID!) {
    build(id: $id) {
      id
      bindConnections {
        place
        name
        moduleName
        kind
        version
        delegatedOnly
        agentless
        problem
        fields {
          ...ModuleFieldDefFields
        }
      }
    }
  }
`)

export const AddMinionTargetMutation = graphql(`
  mutation AddMinionTarget($buildId: ID!, $conn: Int!, $input: MinionTargetInput!, $sessionId: ID) {
    addMinionTarget(buildId: $buildId, conn: $conn, input: $input, sessionId: $sessionId) {
      ...MinionTargetFields
    }
  }
`)

export const UpdateMinionTargetMutation = graphql(`
  mutation UpdateMinionTarget($id: ID!, $input: MinionTargetInput!) {
    updateMinionTarget(id: $id, input: $input) {
      ...MinionTargetFields
    }
  }
`)

export const CheckMinionTargetMutation = graphql(`
  mutation CheckMinionTarget($id: ID!) {
    checkMinionTarget(id: $id) {
      ...MinionTargetFields
    }
  }
`)

export const ConnectMinionTargetMutation = graphql(`
  mutation ConnectMinionTarget($id: ID!) {
    connectMinionTarget(id: $id) {
      ...MinionTargetFields
    }
  }
`)

export const DisconnectMinionTargetMutation = graphql(`
  mutation DisconnectMinionTarget($id: ID!) {
    disconnectMinionTarget(id: $id) {
      ...MinionTargetFields
    }
  }
`)

export const RemoveMinionTargetMutation = graphql(`
  mutation RemoveMinionTarget($id: ID!) {
    removeMinionTarget(id: $id)
  }
`)

export const MinionTargetChangedSubscription = graphql(`
  subscription MinionTargetChanged($operationId: ID!) {
    minionTargetChanged(operationId: $operationId) {
      action
      targetId
      operationId
    }
  }
`)

// The sessions whose minions can be a target's delegate: their builds have
// a delegate command serving the target's channel kind. The server
// searches.
export const DelegateSessionsQuery = graphql(`
  query DelegateSessions($operationId: ID!, $delegatesFor: MinionDelegatesForInput!, $search: String, $first: Int, $after: String) {
    minionSessions(operationId: $operationId, delegatesFor: $delegatesFor, search: $search, first: $first, after: $after) {
      edges {
        node {
          id
          number
          host
          user
          moduleName
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

// The target a bind-call session is the page of: its URL and state on the
// session's Connections card.
export const MinionSessionTargetQuery = graphql(`
  query MinionSessionTarget($id: ID!) {
    minionSession(id: $id) {
      id
      operationId
      connKind
      bindTarget {
        ...MinionTargetFields
      }
    }
  }
`)

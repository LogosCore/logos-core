import { graphql } from "@/graphql/gql"

// An operation's reverse port forwards' listeners: the ports its tasks'
// minions listen on (core: minion_listeners.graphql). The list a page at a
// time, kept live by the operation's listener events: opened, where the
// minion listens, their streams' numbers, closed.
export const MinionListenerFields = graphql(`
  fragment MinionListenerFields on MinionListener {
    id
    bind
    listen
    bound
    udp
    front
    to
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

export const MinionListenersQuery = graphql(`
  query MinionListeners($operationId: ID!, $search: String, $first: Int, $after: String) {
    minionListeners(operationId: $operationId, search: $search, first: $first, after: $after) {
      edges {
        node {
          ...MinionListenerFields
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

export const MinionListenerChangedSubscription = graphql(`
  subscription MinionListenerChanged($operationId: ID!) {
    minionListenerChanged(operationId: $operationId) {
      action
      id
      operationId
      listener {
        ...MinionListenerFields
      }
    }
  }
`)

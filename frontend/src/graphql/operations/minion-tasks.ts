import { graphql } from "@/graphql/gql"

// The commands operators give a session's minion, and what it answered
// (core: minion_tasks.graphql).
export const MinionTaskFields = graphql(`
  fragment MinionTaskFields on MinionTask {
    id
    sessionId
    operationId
    command
    args
    issuedBy
    issuedByName
    state
    queuedAt
    sentAt
    sendCount
    ackedAt
    startedAt
    doneAt
    cancelRequestedAt
    ok
    exit
    err
    output
    outputSize
    outputUtf8
    outputTruncated
    data
  }
`)

export const MinionTasksQuery = graphql(`
  query MinionTasks($sessionId: ID!, $last: Int, $before: String) {
    minionTasks(sessionId: $sessionId, last: $last, before: $before) {
      tasks {
        ...MinionTaskFields
      }
      before
    }
  }
`)

export const MinionSessionCommandsQuery = graphql(`
  query MinionSessionCommands($sessionId: ID!) {
    minionSessionCommands(sessionId: $sessionId) {
      id
      label
      description
      params {
        ...ModuleFieldDefFields
      }
    }
  }
`)

export const QueueMinionTaskMutation = graphql(`
  mutation QueueMinionTask($sessionId: ID!, $command: String!, $args: JSON) {
    queueMinionTask(sessionId: $sessionId, command: $command, args: $args) {
      ...MinionTaskFields
    }
  }
`)

export const CancelMinionTaskMutation = graphql(`
  mutation CancelMinionTask($id: ID!) {
    cancelMinionTask(id: $id) {
      ...MinionTaskFields
    }
  }
`)

export const MinionTaskChangedSubscription = graphql(`
  subscription MinionTaskChanged($sessionId: ID!) {
    minionTaskChanged(sessionId: $sessionId) {
      ...MinionTaskFields
    }
  }
`)

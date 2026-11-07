import { graphql } from "@/graphql/gql"

// The commands operators give a session's minion, and what it answered
// (core: minion_tasks.graphql). Lists and live updates carry a task without
// its output; the console reads the output of the task it shows
// (MinionTaskOutput) again as parts come.
export const MinionTaskFields = graphql(`
  fragment MinionTaskFields on MinionTask {
    id
    rev
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
    cancelReason
    ok
    exit
    err
    outputSize
    outputTruncated
    parts
    dataParts
    result
    action
    dataSize
    dataTruncated
    dataProblem
    missingParts
    feedbackErrors
    feedbackWarnings
    as {
      user
      domain
      integrity
      how
      credentialId
      taskId
      since
    }
    file {
      id
      name
      size
      received
      sha256
      state
      err
      contentType
      doneAt
    }
    upload {
      fileId
      name
      size
      sha256
      acked
      state
      firstAckAt
      lastAckAt
      stream
      sized
      received
      uploaderState
      from
      attachedAt
      uploaderAt
    }
    stream {
      name
      size
      sent
      delivered
      state
      sha256
      announcedAt
      from
      attachedAt
      readerAt
    }
    tunnel {
      port
      address
      purpose
      to
      bind
      listen
      bound
      udp
      listening
      stopsAt
      open
      streams
      kept
      up
      down
    }
  }
`)

// A tunnel task's streams, the latest first a page at a time: who reached
// what through its proxy (core: minion_tasks.graphql).
export const MinionTunnelStreamsQuery = graphql(`
  query MinionTunnelStreams($taskId: ID!, $last: Int, $before: String, $search: String, $state: String) {
    minionTunnelStreams(taskId: $taskId, last: $last, before: $before, search: $search, state: $state) {
      streams {
        stream
        client
        host
        port
        udp
        openedAt
        state
        result
        bound
        msg
        endedAt
        end
        up
        down
      }
      before
    }
  }
`)

export const MinionTaskOutputQuery = graphql(`
  query MinionTaskOutput($id: ID!) {
    minionTask(id: $id) {
      id
      output
      outputUtf8
      data
    }
  }
`)

// What its minion said of a task as feedback (the latest 20), read apart
// from its output: it comes with the session's feedback, not its parts.
export const MinionTaskFeedbackQuery = graphql(`
  query MinionTaskFeedback($id: ID!) {
    minionTask(id: $id) {
      id
      feedback {
        ...MinionFeedbackFields
      }
    }
  }
`)

export const MinionFileStringsQuery = graphql(`
  query MinionFileStrings($fileId: ID!, $after: Int, $first: Int, $minLength: Int, $encoding: String, $search: String) {
    minionFileStrings(fileId: $fileId, after: $after, first: $first, minLength: $minLength, encoding: $encoding, search: $search) {
      strings {
        offset
        length
        encoding
        text
      }
      next
      done
    }
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

// The task as core keeps it, for the console's JSON tab (debugging).
export const MinionTaskDocumentQuery = graphql(`
  query MinionTaskDocument($id: ID!) {
    minionTask(id: $id) {
      id
      document
    }
  }
`)

export const MinionSessionCommandsQuery = graphql(`
  query MinionSessionCommands($sessionId: ID!) {
    minionSessionCommands(sessionId: $sessionId) {
      id
      label
      description
      result
      action
      kinds
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

export const MinionStreamLinkMutation = graphql(`
  mutation MinionStreamLink($taskId: ID!) {
    minionStreamLink(taskId: $taskId) {
      path
      expiresAt
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

import { graphql } from "@/graphql/gql"

// A minion session as the Sessions table shows it: read-only, opened by a
// minion's hello.
export const MinionSessionFields = graphql(`
  fragment MinionSessionFields on MinionSession {
    id
    rev
    state
    archived
    folderId
    mark {
      color
      icon
      emoji
    }
    number
    operationId
    buildId
    buildDescription
    description
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
    machineId
    integrity
    acting {
      user
      domain
      integrity
      how
      credentialId
      taskId
      since
    }
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
    via {
      sessionId
      number
      taskId
    }
    viaSince
    version
    x
    createdAt
    lastSeenAt
    connected
    conn
    connKind
    connName
    connCount
    connChanges {
      at
      from
      to
      reason
    }
    maxTasks
    maxTasksSettable
    maxTasksPending
    maxTasksNotTaken
    feedbackErrors
    feedbackWarnings
    feedbackAt
    feedbackReadAt
  }
`)

export const MinionFeedbackFields = graphql(`
  fragment MinionFeedbackFields on MinionFeedback {
    id
    level
    msg
    code
    taskId
    at
    count
    receivedAt
    unread
  }
`)

// A session's feedback, newest first, a page at a time.
export const MinionFeedbackQuery = graphql(`
  query MinionFeedback($sessionId: ID!, $level: String, $search: String, $after: String, $first: Int) {
    minionFeedback(sessionId: $sessionId, level: $level, search: $search, after: $after, first: $first) {
      entries {
        ...MinionFeedbackFields
      }
      next
    }
  }
`)

export const MinionIdentitiesQuery = graphql(`
  query MinionIdentities($sessionId: ID!, $after: String, $first: Int) {
    minionIdentities(sessionId: $sessionId, after: $after, first: $first) {
      entries {
        id
        at
        scope
        user
        domain
        integrity
        how
        credentialId
        taskId
      }
      next
    }
  }
`)

export const MarkMinionFeedbackReadMutation = graphql(`
  mutation MarkMinionFeedbackRead($id: ID!) {
    markMinionFeedbackRead(id: $id) {
      ...MinionSessionFields
    }
  }
`)

export const MinionSessionsQuery = graphql(`
  query MinionSessions(
    $operationId: ID!
    $archived: Boolean
    $folderId: ID
    $search: String
    $filters: [MinionSessionFilterInput!]
    $sort: MinionSessionSortInput
    $first: Int
    $after: String
  ) {
    minionSessions(
      operationId: $operationId
      archived: $archived
      folderId: $folderId
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
    consoleSplit
    filesSplit
    paneTextSize
    promptFx
    widths {
      column
      px
    }
    paneParked
    folders {
      operationId
      folderId
    }
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

export const SetMinionSessionMaxTasksMutation = graphql(`
  mutation SetMinionSessionMaxTasks($id: ID!, $max: Int!) {
    setMinionSessionMaxTasks(id: $id, max: $max) {
      ...MinionSessionFields
    }
  }
`)

// How many of the session's tasks the minion has and has not ended, those
// holding a place in its max tasks, and its tunnels apart: read apart from
// the session (counts, for one session at a time).
export const MinionSessionRunningTasksQuery = graphql(`
  query MinionSessionRunningTasks($id: ID!) {
    minionSession(id: $id) {
      id
      runningTasks
      runningTunnels
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

// The session's build's connections: fixed with the build, so apart from
// the fields live updates replace.
export const MinionSessionConnectionsQuery = graphql(`
  query MinionSessionConnections($id: ID!) {
    minionSession(id: $id) {
      id
      connections {
        place
        name
        moduleName
        kind
        params
      }
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

export const SetMinionSessionsArchivedMutation = graphql(`
  mutation SetMinionSessionsArchived(
    $operationId: ID!
    $ids: [ID!]
    $matching: MinionSessionMatchInput
    $archived: Boolean!
  ) {
    setMinionSessionsArchived(operationId: $operationId, ids: $ids, matching: $matching, archived: $archived)
  }
`)

export const SetMinionSessionDescriptionMutation = graphql(`
  mutation SetMinionSessionDescription($id: ID!, $description: String!) {
    setMinionSessionDescription(id: $id, description: $description) {
      ...MinionSessionFields
    }
  }
`)

export const SetMinionSessionMarkMutation = graphql(`
  mutation SetMinionSessionMark($id: ID!, $mark: MinionSessionMarkInput) {
    setMinionSessionMark(id: $id, mark: $mark) {
      ...MinionSessionFields
    }
  }
`)

export const SetMinionSessionsMarkMutation = graphql(`
  mutation SetMinionSessionsMark(
    $operationId: ID!
    $ids: [ID!]
    $matching: MinionSessionMatchInput
    $mark: MinionSessionMarkInput
  ) {
    setMinionSessionsMark(operationId: $operationId, ids: $ids, matching: $matching, mark: $mark)
  }
`)

export const MinionSessionFolderFieldsFragment = graphql(`
  fragment MinionSessionFolderFields on MinionSessionFolder {
    id
    operationId
    name
    icon
    emoji
    color
    order
    sessions
  }
`)

export const MinionSessionFoldersQuery = graphql(`
  query MinionSessionFolders($operationId: ID!) {
    minionSessionFolders(operationId: $operationId) {
      ...MinionSessionFolderFields
    }
  }
`)

export const CreateMinionSessionFolderMutation = graphql(`
  mutation CreateMinionSessionFolder($operationId: ID!, $input: MinionSessionFolderInput!) {
    createMinionSessionFolder(operationId: $operationId, input: $input) {
      ...MinionSessionFolderFields
    }
  }
`)

export const UpdateMinionSessionFolderMutation = graphql(`
  mutation UpdateMinionSessionFolder($id: ID!, $input: MinionSessionFolderInput!) {
    updateMinionSessionFolder(id: $id, input: $input) {
      ...MinionSessionFolderFields
    }
  }
`)

export const MoveMinionSessionFolderMutation = graphql(`
  mutation MoveMinionSessionFolder($id: ID!, $before: ID) {
    moveMinionSessionFolder(id: $id, before: $before) {
      ...MinionSessionFolderFields
    }
  }
`)

export const DeleteMinionSessionFolderMutation = graphql(`
  mutation DeleteMinionSessionFolder($id: ID!) {
    deleteMinionSessionFolder(id: $id)
  }
`)

export const SetMinionSessionsFolderMutation = graphql(`
  mutation SetMinionSessionsFolder($operationId: ID!, $ids: [ID!], $matching: MinionSessionMatchInput, $folderId: ID) {
    setMinionSessionsFolder(operationId: $operationId, ids: $ids, matching: $matching, folderId: $folderId)
  }
`)

export const MinionSessionCountQuery = graphql(`
  query MinionSessionCount($operationId: ID!) {
    minionSessionCount(operationId: $operationId)
  }
`)

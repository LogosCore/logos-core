import { graphql } from "@/graphql/gql"

// The file browser (core: file_browser.graphql): what core's minions have
// seen of a machine's files, asked for by one of its sessions.

export const FsTaskRefFields = graphql(`
  fragment FsTaskRefFields on FsTaskRef {
    taskId
    sessionId
    number
    user
  }
`)

export const FsEntryFields = graphql(`
  fragment FsEntryFields on FsEntry {
    path
    name
    parent
    sep
    type
    size
    modified
    mode
    owner
    group
    target
    hidden
    known
    seenAt
    seenBy {
      ...FsTaskRefFields
    }
    goneAt
    goneBy {
      ...FsTaskRefFields
    }
    goneHow
    listedAt
    listedBy {
      ...FsTaskRefFields
    }
    listErr
    listErrAt
    listErrBy {
      ...FsTaskRefFields
    }
    downloaded {
      taskId
      fileId
      size
      sha256
      stream
      at
    }
    uploaded {
      taskId
      size
      sha256
      stream
      by
      at
    }
  }
`)

export const FileBrowserQuery = graphql(`
  query FileBrowser($sessionId: ID!) {
    fileBrowser(sessionId: $sessionId) {
      key
      sessions {
        id
        number
        host
        user
        state
        lastSeenAt
      }
    }
  }
`)

export const FileBrowserEntriesQuery = graphql(`
  query FileBrowserEntries(
    $sessionId: ID!
    $path: String
    $after: String
    $first: Int
    $search: String
    $sort: String
    $desc: Boolean
    $showDeleted: Boolean
    $foldersOnly: Boolean
  ) {
    fileBrowserEntries(
      sessionId: $sessionId
      path: $path
      after: $after
      first: $first
      search: $search
      sort: $sort
      desc: $desc
      showDeleted: $showDeleted
      foldersOnly: $foldersOnly
    ) {
      entries {
        ...FsEntryFields
      }
      next
      folder {
        ...FsEntryFields
      }
    }
  }
`)

export const FileBrowserSearchQuery = graphql(`
  query FileBrowserSearch($sessionId: ID!, $query: String!, $after: String, $first: Int, $showDeleted: Boolean) {
    fileBrowserSearch(sessionId: $sessionId, query: $query, after: $after, first: $first, showDeleted: $showDeleted) {
      entries {
        ...FsEntryFields
      }
      next
    }
  }
`)

export const FileBrowserChangedSubscription = graphql(`
  subscription FileBrowserChanged($sessionId: ID!) {
    fileBrowserChanged(sessionId: $sessionId) {
      folder
    }
  }
`)

export const MinionTaskQuery = graphql(`
  query MinionTask($id: ID!) {
    minionTask(id: $id) {
      ...MinionTaskFields
    }
  }
`)

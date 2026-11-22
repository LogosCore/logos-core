import { graphql } from "@/graphql/gql"

// The admins' Storage page (core: storage.graphql): the object store core
// keeps its files in, as core read it at most a minute ago; what core keeps
// in each bucket, by operation; the store's maintenance; and the retention
// cleanups of recordings, an admin's or the retention policy's.

export const StorageStatusQuery = graphql(`
  query StorageStatus {
    storageStatus {
      at
      recordsError
      seaweedfs {
        configured
        reachable
        error
        version
        leader
        slots
        freeSlots
      }
      disks {
        node
        dir
        error
        total
        used
        free
        volumes
        maxVolumes
      }
      buckets {
        name
        purposes
        volumes
        writableVolumes
        readOnlyVolumes
        size
        deletedBytes
        objects
        objectBytes
      }
      warnings {
        level
        code
        bucket
        message
      }
    }
  }
`)

// The sidebar's mark: the warnings alone.
export const StorageWarningsQuery = graphql(`
  query StorageWarnings {
    storageStatus {
      warnings {
        level
      }
    }
  }
`)

export const StorageConsumersQuery = graphql(`
  query StorageConsumers($bucket: String!, $search: String, $first: Int, $after: String) {
    storageConsumers(bucket: $bucket, search: $search, first: $first, after: $after) {
      edges {
        cursor
        node {
          operationId
          operationName
          purpose
          kind
          objects
          bytes
        }
      }
      pageInfo {
        hasNextPage
        endCursor
      }
      totalCount
    }
  }
`)

export const StorageActionFields = graphql(`
  fragment StorageActionFields on StorageAction {
    id
    action
    bucket
    actor {
      type
      id
      name
    }
    status
    error
    startedAt
    finishedAt
    freed
    volumes
    olderThanDays
    before
    operationId
    operationName
    recordings
    bytes
    plannedRecordings
    plannedBytes
    step
    stepAt
    cancelRequestedAt
    cancelledBy {
      type
      id
      name
    }
    vacuumPaused
    reclaimVolumes {
      state
    }
  }
`)

// One action as it is now, with a reclaim's volumes: its details, read again
// while it runs.
export const StorageActionQuery = graphql(`
  query StorageAction($id: ID!) {
    storageAction(id: $id) {
      ...StorageActionFields
      reclaimVolumes {
        id
        bucket
        server
        size
        deletedBytes
        state
        freed
        msg
        startedAt
        finishedAt
      }
    }
  }
`)

export const CancelStorageActionMutation = graphql(`
  mutation CancelStorageAction($id: ID!) {
    cancelStorageAction(id: $id) {
      ...StorageActionFields
    }
  }
`)

export const StorageActionsQuery = graphql(`
  query StorageActions($first: Int, $after: String) {
    storageActions(first: $first, after: $after) {
      edges {
        cursor
        node {
          ...StorageActionFields
        }
      }
      pageInfo {
        hasNextPage
        endCursor
      }
      totalCount
    }
  }
`)

export const ReclaimStorageMutation = graphql(`
  mutation ReclaimStorage {
    reclaimStorage {
      ...StorageActionFields
    }
  }
`)

export const GrowStorageBucketMutation = graphql(`
  mutation GrowStorageBucket($bucket: String!) {
    growStorageBucket(bucket: $bucket) {
      ...StorageActionFields
    }
  }
`)

export const StorageRetentionFields = graphql(`
  fragment StorageRetentionFields on StorageRetention {
    enabled
    olderThanDays
    updatedAt
    updatedBy {
      type
      id
      name
    }
    lastRun {
      ...StorageActionFields
    }
    nextRunAt
  }
`)

export const StorageRetentionQuery = graphql(`
  query StorageRetention {
    storageRetention {
      ...StorageRetentionFields
    }
  }
`)

export const StorageCleanupPreviewQuery = graphql(`
  query StorageCleanupPreview($olderThanDays: Int!, $first: Int, $after: String) {
    storageCleanupPreview(olderThanDays: $olderThanDays, first: $first, after: $after) {
      olderThanDays
      before
      recordings
      bytes
      operations {
        edges {
          cursor
          node {
            operationId
            operationName
            recordings
            bytes
          }
        }
        pageInfo {
          hasNextPage
          endCursor
        }
        totalCount
      }
    }
  }
`)

export const SetStorageRetentionMutation = graphql(`
  mutation SetStorageRetention($enabled: Boolean!, $olderThanDays: Int!) {
    setStorageRetention(enabled: $enabled, olderThanDays: $olderThanDays) {
      ...StorageRetentionFields
    }
  }
`)

export const CleanUpStorageMutation = graphql(`
  mutation CleanUpStorage($olderThanDays: Int!, $operationId: ID) {
    cleanUpStorage(olderThanDays: $olderThanDays, operationId: $operationId) {
      ...StorageActionFields
    }
  }
`)

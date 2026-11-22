import { graphql } from "@/graphql/gql"

// The admins' Storage page (core: storage.graphql): the object store core
// keeps its files in, as core read it at most a minute ago; what core keeps
// in each bucket, by operation; and the store's maintenance.

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

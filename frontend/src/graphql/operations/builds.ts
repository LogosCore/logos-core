import { graphql } from "@/graphql/gql"

export const BuildSchemaFields = graphql(`
  fragment BuildSchemaFields on BuildSchema {
    version
    crypto
    persistedSessions
    failover
    maxTasks
    compression
    errorCodesPerBuild
    fields {
      ...ModuleFieldDefFields
    }
    channels {
      moduleName
      num
      versions
      kinds {
        kind
        fields {
          ...ModuleFieldDefFields
        }
      }
    }
    targets {
      id
      label
      description
      format
      platforms
      fields {
        ...ModuleFieldDefFields
      }
      channels {
        moduleName
        versions
        kinds
      }
    }
    commands {
      id
      op
      label
      description
      targets
      default
      required
      kinds
      params {
        ...ModuleFieldDefFields
      }
    }
    errorCodes {
      code
      label
      description
      fixed
    }
  }
`)

export const BuildFileFields = graphql(`
  fragment BuildFileFields on BuildFile {
    id
    operationId
    name
    size
    sha256
    mediaType
    createdAt
  }
`)

export const BuildFields = graphql(`
  fragment BuildFields on Build {
    id
    operationId
    persistedSession
    description
    moduleName
    target
    platform
    schemaVersion
    state
    factoryVersion
    stages {
      id
      label
      core
      state
      message
      startedAt
      finishedAt
    }
    artifact {
      filename
      size
      sha256
      mediaType
      deleted
    }
    error {
      code
      message
    }
    createdBy {
      type
      id
      name
    }
    createdAt
    startedAt
    finishedAt
    deadline
  }
`)

export const FactoryKindsQuery = graphql(`
  query FactoryKinds {
    factoryKinds {
      moduleName
      icon
      iconLight
      instances
      schema {
        ...BuildSchemaFields
      }
    }
  }
`)

export const BuildFilesQuery = graphql(`
  query BuildFiles($operationId: ID!) {
    buildFiles(operationId: $operationId) {
      ...BuildFileFields
    }
  }
`)

export const BuildsQuery = graphql(`
  query Builds(
    $operationId: ID!
    $search: String
    $states: [String!]
    $moduleNames: [String!]
    $first: Int
    $after: String
  ) {
    builds(
      operationId: $operationId
      search: $search
      states: $states
      moduleNames: $moduleNames
      first: $first
      after: $after
    ) {
      edges {
        node {
          ...BuildFields
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

export const BuildQuery = graphql(`
  query Build($id: ID!) {
    build(id: $id) {
      ...BuildFields
      values
      commands
      connections
      connectionDescriptions
      failover
      maxTasks
      compression
      errorCodes {
        exitCode
        code
        label
        description
        fixed
      }
      uniqueErrorCodes
      crypto
      envelope
      codec
      warnings
      log
      cancelledBy {
        type
        id
        name
      }
    }
  }
`)

// What core sent the factory, fetched apart: the JSON tab's Sent to factory
// view asks for it, so the build's own query (polled while it runs) does not
// carry it.
export const BuildRequestQuery = graphql(`
  query BuildRequest($id: ID!) {
    build(id: $id) {
      id
      request
      instance
      startedAt
    }
  }
`)

export const CreateBuildMutation = graphql(`
  mutation CreateBuild($operationId: ID!, $input: BuildInput!) {
    createBuild(operationId: $operationId, input: $input) {
      ...BuildFields
    }
  }
`)

export const CancelBuildMutation = graphql(`
  mutation CancelBuild($id: ID!) {
    cancelBuild(id: $id) {
      ...BuildFields
    }
  }
`)

export const DeleteBuildMutation = graphql(`
  mutation DeleteBuild($id: ID!) {
    deleteBuild(id: $id)
  }
`)

export const DeleteBuildsMutation = graphql(`
  mutation DeleteBuilds($operationId: ID!, $ids: [ID!], $matching: BuildMatchInput) {
    deleteBuilds(operationId: $operationId, ids: $ids, matching: $matching)
  }
`)

export const DeleteBuildArtifactMutation = graphql(`
  mutation DeleteBuildArtifact($id: ID!) {
    deleteBuildArtifact(id: $id) {
      ...BuildFields
    }
  }
`)

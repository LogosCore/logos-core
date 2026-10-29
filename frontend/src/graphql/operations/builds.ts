import { graphql } from "@/graphql/gql"

export const BuildSchemaFields = graphql(`
  fragment BuildSchemaFields on BuildSchema {
    version
    crypto
    fields {
      ...ModuleFieldDefFields
    }
    channels {
      moduleName
      versions
      fields {
        ...ModuleFieldDefFields
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
      }
    }
    commands {
      id
      label
      description
      targets
      default
      required
      params {
        ...ModuleFieldDefFields
      }
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

export const DeleteBuildFileMutation = graphql(`
  mutation DeleteBuildFile($id: ID!) {
    deleteBuildFile(id: $id)
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
      crypto
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

export const DeleteBuildArtifactMutation = graphql(`
  mutation DeleteBuildArtifact($id: ID!) {
    deleteBuildArtifact(id: $id) {
      ...BuildFields
    }
  }
`)

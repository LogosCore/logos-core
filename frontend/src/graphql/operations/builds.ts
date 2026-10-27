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
    minionId
    state
    instance
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
    capabilities
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
  query Builds($operationId: ID!, $limit: Int) {
    builds(operationId: $operationId, limit: $limit) {
      ...BuildFields
    }
  }
`)

export const BuildQuery = graphql(`
  query Build($id: ID!) {
    build(id: $id) {
      ...BuildFields
      values
      connections
      connectionDescriptions
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

export const DeleteBuildArtifactMutation = graphql(`
  mutation DeleteBuildArtifact($id: ID!) {
    deleteBuildArtifact(id: $id) {
      ...BuildFields
    }
  }
`)

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

export const BuildPresetFields = graphql(`
  fragment BuildPresetFields on BuildPreset {
    id
    operationId
    name
    description
    moduleName
    target
    platform
    values
    connections {
      endpointId
      endpointName
      values
    }
    schemaVersion
    schemaChanged
    problems {
      key
      message
    }
    revision
    updatedBy {
      type
      id
      name
    }
    updatedAt
  }
`)

export const BuildFields = graphql(`
  fragment BuildFields on Build {
    id
    operationId
    moduleName
    target
    platform
    schemaVersion
    minionId
    state
    instance
    factoryVersion
    steps {
      step
      message
      percent
      at
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

export const BuildPresetsQuery = graphql(`
  query BuildPresets($operationId: ID!) {
    buildPresets(operationId: $operationId) {
      ...BuildPresetFields
    }
  }
`)

export const BuildPresetQuery = graphql(`
  query BuildPreset($id: ID!) {
    buildPreset(id: $id) {
      ...BuildPresetFields
    }
  }
`)

export const CreateBuildPresetMutation = graphql(`
  mutation CreateBuildPreset($operationId: ID!, $moduleName: String!, $input: BuildPresetInput!) {
    createBuildPreset(operationId: $operationId, moduleName: $moduleName, input: $input) {
      ...BuildPresetFields
    }
  }
`)

export const UpdateBuildPresetMutation = graphql(`
  mutation UpdateBuildPreset($id: ID!, $input: BuildPresetInput!, $expectedRevision: Int!) {
    updateBuildPreset(id: $id, input: $input, expectedRevision: $expectedRevision) {
      ...BuildPresetFields
    }
  }
`)

export const DeleteBuildPresetMutation = graphql(`
  mutation DeleteBuildPreset($id: ID!) {
    deleteBuildPreset(id: $id)
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
      warnings
      log
      presetId
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

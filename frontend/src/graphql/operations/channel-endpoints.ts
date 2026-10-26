import { graphql } from "@/graphql/gql"

export const ChannelEndpointFields = graphql(`
  fragment ChannelEndpointFields on ChannelEndpoint {
    id
    operationId
    name
    description
    moduleName
    schemaVersion
    schemaChanged
    values
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
    createdAt
    updatedAt
  }
`)

export const ChannelKindsQuery = graphql(`
  query ChannelKinds {
    channelKinds {
      moduleName
      icon
      schema {
        version
        fields {
          ...ModuleFieldDefFields
        }
      }
    }
  }
`)

export const ChannelEndpointsQuery = graphql(`
  query ChannelEndpoints($operationId: ID!, $moduleName: String) {
    channelEndpoints(operationId: $operationId, moduleName: $moduleName) {
      ...ChannelEndpointFields
    }
  }
`)

export const ChannelEndpointQuery = graphql(`
  query ChannelEndpoint($id: ID!) {
    channelEndpoint(id: $id) {
      ...ChannelEndpointFields
    }
  }
`)

export const ChannelEndpointExportQuery = graphql(`
  query ChannelEndpointExport($id: ID!) {
    channelEndpointExport(id: $id)
  }
`)

export const CreateChannelEndpointMutation = graphql(`
  mutation CreateChannelEndpoint($operationId: ID!, $moduleName: String!, $input: ChannelEndpointInput!) {
    createChannelEndpoint(operationId: $operationId, moduleName: $moduleName, input: $input) {
      ...ChannelEndpointFields
    }
  }
`)

export const UpdateChannelEndpointMutation = graphql(`
  mutation UpdateChannelEndpoint($id: ID!, $input: ChannelEndpointInput!, $expectedRevision: Int!) {
    updateChannelEndpoint(id: $id, input: $input, expectedRevision: $expectedRevision) {
      ...ChannelEndpointFields
    }
  }
`)

export const DeleteChannelEndpointMutation = graphql(`
  mutation DeleteChannelEndpoint($id: ID!) {
    deleteChannelEndpoint(id: $id)
  }
`)

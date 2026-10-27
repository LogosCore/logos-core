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
      iconLight
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
  query ChannelEndpoints($operationId: ID!, $search: String, $moduleNames: [String!], $first: Int, $after: String) {
    channelEndpoints(
      operationId: $operationId
      search: $search
      moduleNames: $moduleNames
      first: $first
      after: $after
    ) {
      edges {
        node {
          ...ChannelEndpointFields
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

export const FreeChannelEndpointNameQuery = graphql(`
  query FreeChannelEndpointName($operationId: ID!, $name: String!, $tag: String) {
    freeChannelEndpointName(operationId: $operationId, name: $name, tag: $tag)
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

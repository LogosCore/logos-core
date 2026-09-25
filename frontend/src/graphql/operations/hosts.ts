import { graphql } from "@/graphql/gql"

// Row fragment for the Hosts table. Unlike hashes (which leave the linked
// credential off the row), interfaces/routes ARE the host's primary content
// and are small bounded lists — loading them inline lets the edit dialog
// prefill straight from the clicked row's cached node, no follow-up query.
// `operation` is intentionally skipped: it's a per-row DB lookup and the
// Hosts tab only exists in operation-scoped mode where the parent is implicit.
export const HostFields = graphql(`
  fragment HostFields on Host {
    id
    operationId
    hostname
    description
    os
    emoji
    icon
    color
    interfaces {
      name
      mac
      addresses
    }
    routes {
      destination
      gateway
      interface
    }
    logins {
      user
      from
      tty
      lastSeen
      count
    }
    createdBy {
      id
      username
    }
    createdAt
    updatedAt
  }
`)

export const HostsQuery = graphql(`
  query Hosts(
    $operationId: ID!
    $search: String
    $sortBy: HostSortField
    $sortDirection: SortDirection
    $first: Int
    $after: String
  ) {
    hosts(
      operationId: $operationId
      search: $search
      sortBy: $sortBy
      sortDirection: $sortDirection
      first: $first
      after: $after
    ) {
      edges {
        node {
          ...HostFields
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

// Single-host detail query. Backs the inline /host wiki reference chip, which
// persists only a hostId and resolves the host live (so renames / topology
// edits flow through without rewriting the document). Sibling of HashQuery.
export const HostQuery = graphql(`
  query Host($id: ID!) {
    host(id: $id) {
      ...HostFields
    }
  }
`)

export const CreateHostMutation = graphql(`
  mutation CreateHost($operationId: ID!, $input: CreateHostInput!) {
    createHost(operationId: $operationId, input: $input) {
      ...HostFields
    }
  }
`)

export const UpdateHostMutation = graphql(`
  mutation UpdateHost($id: ID!, $input: UpdateHostInput!) {
    updateHost(id: $id, input: $input) {
      ...HostFields
    }
  }
`)

export const DeleteHostMutation = graphql(`
  mutation DeleteHost($id: ID!) {
    deleteHost(id: $id)
  }
`)

// The handler patches the host into the cached lists, detail and topology
// rather than refetching them, so it selects the row. The server reads the
// host for every non-delete event whether or not it is selected.
export const HostChangedSubscription = graphql(`
  subscription HostChanged($operationId: ID!) {
    hostChanged(operationId: $operationId) {
      action
      hostId
      host {
        ...HostFields
      }
    }
  }
`)

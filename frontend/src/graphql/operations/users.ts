import { graphql } from "@/graphql/gql"

export const UserFields = graphql(`
  fragment UserFields on User {
    id
    username
    roles
    active
    authSource
    createdAt
    updatedAt
  }
`)

export const MeQuery = graphql(`
  query Me {
    me {
      ...UserFields
      hiddenIdentities
      skillDownloadedVersion
      skillDownloadedAt
      skillUpdateSnoozedVersion
      completedGuides
      frequentIcons
      recentOperations {
        id
        name
        description
      }
    }
  }
`)

// Records that the caller finished or dismissed one in-app guide. Idempotent on
// the server, so calling it twice is harmless.
export const CompleteGuideMutation = graphql(`
  mutation CompleteGuide($guide: String!) {
    completeGuide(guide: $guide) {
      id
      completedGuides
    }
  }
`)

// Counts one icon pick for the icon picker's "Frequently used" row. Returns
// the re-ranked list so the cache can take it as-is.
export const RecordIconUseMutation = graphql(`
  mutation RecordIconUse($name: String!) {
    recordIconUse(name: $name) {
      id
      frequentIcons
    }
  }
`)

// Moves an operation to the front of the caller's recent operations.
export const TouchRecentOperationMutation = graphql(`
  mutation TouchRecentOperation($operationId: ID!) {
    touchRecentOperation(operationId: $operationId) {
      id
      recentOperations {
        id
        name
        description
      }
    }
  }
`)

// One-time move of the picker history older builds kept in localStorage.
// Each list is written only if the server-side one is still empty.
export const ImportLocalPreferencesMutation = graphql(`
  mutation ImportLocalPreferences($input: ImportLocalPreferencesInput!) {
    importLocalPreferences(input: $input) {
      id
      frequentIcons
      recentOperations {
        id
        name
        description
      }
    }
  }
`)

export const UserQuery = graphql(`
  query User($id: ID!) {
    user(id: $id) {
      ...UserFields
    }
  }
`)

export const UsersQuery = graphql(`
  query Users(
    $search: String
    $sortBy: UserSortField
    $sortDirection: SortDirection
    $first: Int
    $after: String
  ) {
    users(
      search: $search
      sortBy: $sortBy
      sortDirection: $sortDirection
      first: $first
      after: $after
    ) {
      edges {
        node {
          ...UserFields
        }
        cursor
      }
      pageInfo {
        hasNextPage
        hasPreviousPage
        startCursor
        endCursor
      }
      totalCount
    }
  }
`)

export const CreateUserMutation = graphql(`
  mutation CreateUser($input: CreateUserInput!) {
    createUser(input: $input) {
      ...UserFields
    }
  }
`)

export const UpdateUserMutation = graphql(`
  mutation UpdateUser($id: ID!, $input: UpdateUserInput!) {
    updateUser(id: $id, input: $input) {
      ...UserFields
    }
  }
`)

export const DeleteUserMutation = graphql(`
  mutation DeleteUser($id: ID!) {
    deleteUser(id: $id)
  }
`)

export const UpdateOwnProfileMutation = graphql(`
  mutation UpdateOwnProfile($input: UpdateUserInput!) {
    updateOwnProfile(input: $input) {
      ...UserFields
    }
  }
`)

// Replaces the caller's hidden-identity list (usernames hidden from the host
// topology Users lens). The server normalizes the names and scopes the write
// to the JWT user, so we only echo back the canonical result.
export const SetHiddenIdentitiesMutation = graphql(`
  mutation SetHiddenIdentities($names: [String!]!) {
    setHiddenIdentities(names: $names) {
      id
      hiddenIdentities
    }
  }
`)

// Real-time subscription — streams user create/update/delete events via SSE.
// The server includes the full User object for CREATE/UPDATE (null on DELETE).
export const UserChangedSubscription = graphql(`
  subscription UserChanged {
    userChanged {
      action
      userId
      username
      user {
        ...UserFields
      }
    }
  }
`)

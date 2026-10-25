import { graphql } from "@/graphql/gql"

export const ModuleFields = graphql(`
  fragment ModuleFields on Module {
    instance
    type
    name
    version
    description
    status
    lastStatus
    registeredAt
    lastHeartbeatAt
    deregisteredAt
    deregisterReason
    declaredDeadAt
  }
`)

export const ModulesQuery = graphql(`
  query Modules($status: [String!]) {
    modules(status: $status) {
      ...ModuleFields
    }
  }
`)

export const RemoveModuleMutation = graphql(`
  mutation RemoveModule($instance: ID!) {
    removeModule(instance: $instance) {
      ...ModuleFields
    }
  }
`)

// Real-time subscription — streams module lifecycle changes (registered /
// deregistered / dead). Admin-only (gated by module:read). The server includes
// the full Module row on every event.
export const ModuleChangedSubscription = graphql(`
  subscription ModuleChanged {
    moduleChanged {
      action
      instance
      module {
        ...ModuleFields
      }
    }
  }
`)

// --- Module management (detail page) ---

export const ModuleFieldDefFields = graphql(`
  fragment ModuleFieldDefFields on ModuleField {
    key
    type
    label
    description
    group
    order
    unit
    placeholder
    required
    default
    min
    max
    pattern
    maxLength
    options {
      value
      label
    }
  }
`)

export const ModuleDetailFields = graphql(`
  fragment ModuleDetailFields on Module {
    ...ModuleFields
    management {
      schemaVersion
      revision
      appliedRevision
      syncState
      lastApplyError
      properties {
        field {
          ...ModuleFieldDefFields
        }
        apply
        effective
        source
        value
        isSet
        rejectedReason
        restartRequired
      }
      actions {
        name
        label
        description
        risk
        confirm
        timeoutSeconds
        params {
          ...ModuleFieldDefFields
        }
      }
      status {
        key
        type
        label
        description
        unit
        value
      }
    }
  }
`)

export const ModuleQuery = graphql(`
  query Module($instance: ID!) {
    module(instance: $instance) {
      ...ModuleDetailFields
    }
  }
`)

export const ModuleActionInvocationFields = graphql(`
  fragment ModuleActionInvocationFields on ModuleActionInvocation {
    id
    instance
    action
    params
    status
    result
    error
    actor {
      type
      id
      name
    }
    startedAt
    finishedAt
  }
`)

export const ModuleActionInvocationsQuery = graphql(`
  query ModuleActionInvocations($instance: ID!, $limit: Int) {
    moduleActionInvocations(instance: $instance, limit: $limit) {
      ...ModuleActionInvocationFields
    }
  }
`)

export const ModuleConfigChangesQuery = graphql(`
  query ModuleConfigChanges($instance: ID!, $limit: Int) {
    moduleConfigChanges(instance: $instance, limit: $limit) {
      id
      instance
      revision
      changes {
        key
        old
        new
      }
      actor {
        type
        id
        name
      }
      at
    }
  }
`)

export const UpdateModuleConfigMutation = graphql(`
  mutation UpdateModuleConfig(
    $instance: ID!
    $values: [ModuleConfigValueInput!]!
    $expectedRevision: Int!
  ) {
    updateModuleConfig(
      instance: $instance
      values: $values
      expectedRevision: $expectedRevision
    ) {
      ...ModuleDetailFields
    }
  }
`)

export const InvokeModuleActionMutation = graphql(`
  mutation InvokeModuleAction($instance: ID!, $action: String!, $params: JSON) {
    invokeModuleAction(instance: $instance, action: $action, params: $params) {
      ...ModuleActionInvocationFields
    }
  }
`)

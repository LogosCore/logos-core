/* eslint-disable */
import type { DocumentTypeDecoration } from '@graphql-typed-document-node/core';
export type Maybe<T> = T | null;
export type InputMaybe<T> = T | null | undefined;
export type Exact<T extends { [key: string]: unknown }> = { [K in keyof T]: T[K] };
export type MakeOptional<T, K extends keyof T> = Omit<T, K> & { [SubKey in K]?: Maybe<T[SubKey]> };
export type MakeMaybe<T, K extends keyof T> = Omit<T, K> & { [SubKey in K]: Maybe<T[SubKey]> };
export type MakeEmpty<T extends { [key: string]: unknown }, K extends keyof T> = { [_ in K]?: never };
export type Incremental<T> = T | { [P in keyof T]?: P extends ' $fragmentName' | '__typename' ? T[P] : never };
/** All built-in and custom scalars, mapped to their actual values */
export type Scalars = {
  ID: { input: string; output: string; }
  String: { input: string; output: string; }
  Boolean: { input: boolean; output: boolean; }
  Int: { input: number; output: number; }
  Float: { input: number; output: number; }
};

export type ApiKey = {
  createdAt: Scalars['String']['output'];
  enabled: Scalars['Boolean']['output'];
  id: Scalars['ID']['output'];
  keyId: Scalars['String']['output'];
  lastUsedAt?: Maybe<Scalars['String']['output']>;
  updatedAt: Scalars['String']['output'];
};

export type ApiKeyWithSecret = {
  apiKey: ApiKey;
  token: Scalars['String']['output'];
};

export type AgentAction = {
  agentKeyId: Scalars['ID']['output'];
  agentName: Scalars['String']['output'];
  arguments: Scalars['String']['output'];
  durationMs: Scalars['Int']['output'];
  error?: Maybe<Scalars['String']['output']>;
  id: Scalars['ID']['output'];
  occurredAt: Scalars['String']['output'];
  operation?: Maybe<Operation>;
  operationId?: Maybe<Scalars['ID']['output']>;
  outcome: AgentActionOutcome;
  tool: Scalars['String']['output'];
  write: Scalars['Boolean']['output'];
};

export type AgentActionConnection = {
  edges: Array<AgentActionEdge>;
  pageInfo: PageInfo;
  totalCount: Scalars['Int']['output'];
};

export type AgentActionEdge = {
  cursor: Scalars['String']['output'];
  node: AgentAction;
};

export type AgentActionOutcome =
  | 'ERROR'
  | 'OK'
  | 'REFUSED';

export type AgentActivityEvent = {
  agentKeyId: Scalars['ID']['output'];
  agentLabel: Scalars['String']['output'];
  agentName: Scalars['String']['output'];
  operationId: Scalars['ID']['output'];
  outcome: Scalars['String']['output'];
  ownerUserId: Scalars['ID']['output'];
  summary: Scalars['String']['output'];
  tool: Scalars['String']['output'];
  write: Scalars['Boolean']['output'];
};

export type AgentActivitySummary = {
  actions: Scalars['Int']['output'];
  agentKeyId: Scalars['ID']['output'];
  agentName: Scalars['String']['output'];
  lastSeen: Scalars['String']['output'];
  operations: Scalars['Int']['output'];
};

export type AgentKey = {
  allowWrites: Scalars['Boolean']['output'];
  createdAt: Scalars['String']['output'];
  enabled: Scalars['Boolean']['output'];
  id: Scalars['ID']['output'];
  keyId: Scalars['String']['output'];
  lastUsedAt?: Maybe<Scalars['String']['output']>;
  maxRole: OperationRole;
  name: Scalars['String']['output'];
  operationScopes: Array<Operation>;
  updatedAt: Scalars['String']['output'];
};

export type AgentKeyWithSecret = {
  agentKey: AgentKey;
  token: Scalars['String']['output'];
};

export type BulkImportHashesInput = {
  comment?: InputMaybe<Scalars['String']['input']>;
  tags?: InputMaybe<Array<Scalars['String']['input']>>;
  text: Scalars['String']['input'];
};

export type BulkImportHashesResult = {
  added: Scalars['Int']['output'];
  hashes: Array<Hash>;
  skipped: Scalars['Int']['output'];
};

export type ChangeTaskStageInput = {
  stage: TaskStage;
  status?: InputMaybe<TaskStatus>;
  summary?: InputMaybe<Scalars['String']['input']>;
  taskId: Scalars['ID']['input'];
};

export type CreateAgentKeyInput = {
  allowWrites?: Scalars['Boolean']['input'];
  maxRole: OperationRole;
  name: Scalars['String']['input'];
  operationScopes?: InputMaybe<Array<Scalars['ID']['input']>>;
};

export type CreateCredentialInput = {
  keys?: InputMaybe<Array<CredentialKeyInput>>;
  name: Scalars['String']['input'];
  password?: InputMaybe<Scalars['String']['input']>;
  properties?: InputMaybe<Array<CredentialPropertyInput>>;
  tags?: InputMaybe<Array<Scalars['String']['input']>>;
  type: CredentialType;
  username?: InputMaybe<Scalars['String']['input']>;
  validity?: InputMaybe<CredentialValidity>;
};

export type CreateCustomTimelineEventInput = {
  color?: InputMaybe<Scalars['String']['input']>;
  description?: InputMaybe<Scalars['String']['input']>;
  emoji?: InputMaybe<Scalars['String']['input']>;
  icon?: InputMaybe<Scalars['String']['input']>;
  name: Scalars['String']['input'];
  occurredAt: Scalars['String']['input'];
};

export type CreateHashInput = {
  comment?: InputMaybe<Scalars['String']['input']>;
  credentialId?: InputMaybe<Scalars['ID']['input']>;
  status?: InputMaybe<HashStatus>;
  tags?: InputMaybe<Array<Scalars['String']['input']>>;
  value: Scalars['String']['input'];
};

export type CreateHostInput = {
  color?: InputMaybe<Scalars['String']['input']>;
  description?: InputMaybe<Scalars['String']['input']>;
  emoji?: InputMaybe<Scalars['String']['input']>;
  hostname: Scalars['String']['input'];
  icon?: InputMaybe<Scalars['String']['input']>;
  interfaces?: InputMaybe<Array<NetworkInterfaceInput>>;
  logins?: InputMaybe<Array<LoginInput>>;
  os?: InputMaybe<Scalars['String']['input']>;
  routes?: InputMaybe<Array<RouteInput>>;
};

export type CreateOperationInput = {
  description?: InputMaybe<Scalars['String']['input']>;
  name: Scalars['String']['input'];
};

export type CreateTaskInput = {
  assigneeIds?: InputMaybe<Array<Scalars['ID']['input']>>;
  credentialReferenceIds?: InputMaybe<Array<Scalars['ID']['input']>>;
  description?: InputMaybe<Scalars['String']['input']>;
  name: Scalars['String']['input'];
  operationId: Scalars['ID']['input'];
  profitDescription?: InputMaybe<Scalars['String']['input']>;
  profitScore: Scalars['Int']['input'];
  riskDescription?: InputMaybe<Scalars['String']['input']>;
  riskScore: Scalars['Int']['input'];
  stage?: InputMaybe<TaskStage>;
  status?: InputMaybe<TaskStatus>;
  summary?: InputMaybe<Scalars['String']['input']>;
  wikiReferenceIds?: InputMaybe<Array<Scalars['ID']['input']>>;
};

export type CreateUserInput = {
  active?: InputMaybe<Scalars['Boolean']['input']>;
  password: Scalars['String']['input'];
  roles: Array<Scalars['String']['input']>;
  username: Scalars['String']['input'];
};

export type CreateWikiDocumentInput = {
  color?: InputMaybe<Scalars['String']['input']>;
  content?: InputMaybe<Scalars['String']['input']>;
  emoji?: InputMaybe<Scalars['String']['input']>;
  icon?: InputMaybe<Scalars['String']['input']>;
  kind?: InputMaybe<WikiDocumentKind>;
  pageType?: InputMaybe<Scalars['String']['input']>;
  parentDocumentId?: InputMaybe<Scalars['ID']['input']>;
  sortOrder?: InputMaybe<Scalars['String']['input']>;
  status?: InputMaybe<WikiDocumentStatus>;
  tags?: InputMaybe<Array<Scalars['String']['input']>>;
  title: Scalars['String']['input'];
};

export type Credential = {
  backlinkCount: Scalars['Int']['output'];
  backlinks: Array<WikiDocument>;
  comments: Array<CredentialComment>;
  createdAt: Scalars['String']['output'];
  createdBy?: Maybe<User>;
  id: Scalars['ID']['output'];
  keys: Array<CredentialKey>;
  name: Scalars['String']['output'];
  operation: Operation;
  operationId: Scalars['ID']['output'];
  password: Scalars['String']['output'];
  properties: Array<CredentialProperty>;
  sourceHashes: Array<Hash>;
  tags: Array<Scalars['String']['output']>;
  taskBacklinks: Array<Task>;
  type: CredentialType;
  updatedAt: Scalars['String']['output'];
  username: Scalars['String']['output'];
  validity: CredentialValidity;
  viewerCanModerateComments: Scalars['Boolean']['output'];
};

export type CredentialComment = {
  author?: Maybe<User>;
  createdAt: Scalars['String']['output'];
  id: Scalars['ID']['output'];
  text: Scalars['String']['output'];
  updatedAt: Scalars['String']['output'];
};

export type CredentialConnection = {
  edges: Array<CredentialEdge>;
  pageInfo: PageInfo;
  totalCount: Scalars['Int']['output'];
};

export type CredentialEdge = {
  cursor: Scalars['String']['output'];
  node: Credential;
};

export type CredentialEvent = {
  action: EventAction;
  credential?: Maybe<Credential>;
  credentialId: Scalars['ID']['output'];
  operationId: Scalars['ID']['output'];
};

export type CredentialKey = {
  content: Scalars['String']['output'];
  name: Scalars['String']['output'];
};

export type CredentialKeyInput = {
  content: Scalars['String']['input'];
  name: Scalars['String']['input'];
};

export type CredentialProperty = {
  name: Scalars['String']['output'];
  value: Scalars['String']['output'];
};

export type CredentialPropertyInput = {
  name: Scalars['String']['input'];
  value: Scalars['String']['input'];
};

export type CredentialSearchField =
  | 'NAME'
  | 'PASSWORD'
  | 'PROPERTIES'
  | 'USERNAME';

export type CredentialSortField =
  | 'CREATED_AT'
  | 'NAME'
  | 'USERNAME';

export type CredentialType =
  | 'API_KEY'
  | 'HASH'
  | 'OTHER'
  | 'PASSWORD'
  | 'SSH_KEY'
  | 'TOKEN';

export type CredentialValidity =
  | 'INVALID'
  | 'UNKNOWN'
  | 'VALID';

export type EventAction =
  | 'CREATED'
  | 'DELETED'
  | 'UPDATED';

export type Hash = {
  backlinkCount: Scalars['Int']['output'];
  backlinks: Array<WikiDocument>;
  comment: Scalars['String']['output'];
  createdAt: Scalars['String']['output'];
  createdBy?: Maybe<User>;
  credential?: Maybe<Credential>;
  credentialId?: Maybe<Scalars['ID']['output']>;
  id: Scalars['ID']['output'];
  operation: Operation;
  operationId: Scalars['ID']['output'];
  status: HashStatus;
  tags: Array<Scalars['String']['output']>;
  updatedAt: Scalars['String']['output'];
  value: Scalars['String']['output'];
};

export type HashConnection = {
  edges: Array<HashEdge>;
  pageInfo: PageInfo;
  totalCount: Scalars['Int']['output'];
};

export type HashEdge = {
  cursor: Scalars['String']['output'];
  node: Hash;
};

export type HashEvent = {
  action: EventAction;
  hash?: Maybe<Hash>;
  hashId: Scalars['ID']['output'];
  operationId: Scalars['ID']['output'];
};

export type HashStatus =
  | 'CRACKED'
  | 'CRACKING'
  | 'FAILED'
  | 'NOT_PROCESSED'
  | 'QUEUED';

export type Host = {
  color: Scalars['String']['output'];
  createdAt: Scalars['String']['output'];
  createdBy?: Maybe<User>;
  description: Scalars['String']['output'];
  emoji: Scalars['String']['output'];
  hostname: Scalars['String']['output'];
  icon: Scalars['String']['output'];
  id: Scalars['ID']['output'];
  interfaces: Array<NetworkInterface>;
  logins: Array<Login>;
  operation: Operation;
  operationId: Scalars['ID']['output'];
  os: Scalars['String']['output'];
  routes: Array<Route>;
  updatedAt: Scalars['String']['output'];
};

export type HostConnection = {
  edges: Array<HostEdge>;
  pageInfo: PageInfo;
  totalCount: Scalars['Int']['output'];
};

export type HostEdge = {
  cursor: Scalars['String']['output'];
  node: Host;
};

export type HostEvent = {
  action: EventAction;
  host?: Maybe<Host>;
  hostId: Scalars['ID']['output'];
  operationId: Scalars['ID']['output'];
};

export type HostSortField =
  | 'CREATED_AT'
  | 'HOSTNAME'
  | 'OS';

export type IconUsageInput = {
  count: Scalars['Int']['input'];
  lastUsedAt: Scalars['String']['input'];
  name: Scalars['String']['input'];
};

export type ImportLocalPreferencesInput = {
  frequentIcons: Array<IconUsageInput>;
  recentOperationIds: Array<Scalars['ID']['input']>;
};

export type Login = {
  count: Scalars['Int']['output'];
  from: Scalars['String']['output'];
  lastSeen: Scalars['String']['output'];
  tty: Scalars['String']['output'];
  user: Scalars['String']['output'];
};

export type LoginInput = {
  count?: InputMaybe<Scalars['Int']['input']>;
  from?: InputMaybe<Scalars['String']['input']>;
  lastSeen?: InputMaybe<Scalars['String']['input']>;
  tty?: InputMaybe<Scalars['String']['input']>;
  user: Scalars['String']['input'];
};

export type MarkHashCrackedInput = {
  credentialId?: InputMaybe<Scalars['ID']['input']>;
  newCredential?: InputMaybe<CreateCredentialInput>;
};

export type Module = {
  declaredDeadAt?: Maybe<Scalars['String']['output']>;
  deregisterReason: Scalars['String']['output'];
  deregisteredAt?: Maybe<Scalars['String']['output']>;
  description: Scalars['String']['output'];
  instance: Scalars['ID']['output'];
  lastHeartbeatAt?: Maybe<Scalars['String']['output']>;
  lastStatus: Scalars['String']['output'];
  name: Scalars['String']['output'];
  registeredAt: Scalars['String']['output'];
  status: Scalars['String']['output'];
  type: Scalars['String']['output'];
  version: Scalars['String']['output'];
};

export type ModuleEvent = {
  action: EventAction;
  instance: Scalars['ID']['output'];
  module?: Maybe<Module>;
};

export type Mutation = {
  addCredentialComment: Credential;
  addOperationMember: Operation;
  addTaskWikiReference: Task;
  adminRevokeAllUserSessions: Scalars['Int']['output'];
  adminRevokeSession: Scalars['Boolean']['output'];
  bulkImportHashes: BulkImportHashesResult;
  changeTaskStage: Task;
  completeGuide: User;
  createAgentKey: AgentKeyWithSecret;
  createCredential: Credential;
  createCustomTimelineEvent: TimelineEvent;
  createHash: Hash;
  createHost: Host;
  createMyAPIKey: ApiKeyWithSecret;
  createOperation: Operation;
  createTask: Task;
  createUser: User;
  createWikiDocument: WikiDocument;
  createWikiDocumentBackup: WikiDocumentBackup;
  deleteAgentKey: Scalars['Boolean']['output'];
  deleteCredential: Scalars['Boolean']['output'];
  deleteCredentialComment: Credential;
  deleteCustomTimelineEvent: Scalars['Boolean']['output'];
  deleteHash: Scalars['Boolean']['output'];
  deleteHost: Scalars['Boolean']['output'];
  deleteMyAPIKey: Scalars['Boolean']['output'];
  deleteOperation: Scalars['Boolean']['output'];
  deleteTask: Scalars['Boolean']['output'];
  deleteUser: Scalars['Boolean']['output'];
  deleteWikiDocument: Scalars['Boolean']['output'];
  deleteWikiDocumentBackup: Scalars['Boolean']['output'];
  duplicateWikiDocument: WikiDocument;
  emptyWikiDocumentTrash: Scalars['Boolean']['output'];
  importLocalPreferences: User;
  instantiateTemplate: WikiDocument;
  markHashCracked: Hash;
  permanentlyDeleteWikiDocument: Scalars['Boolean']['output'];
  publishOperatorFocus: Scalars['Boolean']['output'];
  purgeTask: Scalars['Boolean']['output'];
  recordIconUse: User;
  regenerateAgentKey: AgentKeyWithSecret;
  regenerateMyAPIKey: ApiKeyWithSecret;
  removeModule: Module;
  removeOperationMember: Operation;
  removeSkill: Skill;
  reorderWikiDocumentSiblings: Array<WikiDocument>;
  restoreTask: Task;
  restoreWikiDocument: WikiDocument;
  restoreWikiDocumentBackup: WikiDocument;
  revokeAllMySessions: Scalars['Int']['output'];
  revokeSession: Scalars['Boolean']['output'];
  setAgentKeyEnabled: AgentKey;
  setHiddenIdentities: User;
  setMyAPIKeyEnabled: ApiKey;
  setTaskAssignees: Task;
  setTaskCredentialReferences: Task;
  setTaskWikiReferences: Task;
  setWikiDocumentTemplate: WikiDocument;
  snoozeSkill: Skill;
  snoozeSkillUpdate: User;
  touchRecentOperation: User;
  trackWikiDocumentVisit: WikiDocumentVisit;
  transferSkill: Skill;
  updateAgentKey: AgentKey;
  updateCredential: Credential;
  updateCredentialComment: Credential;
  updateCustomTimelineEvent: TimelineEvent;
  updateHash: Hash;
  updateHost: Host;
  updateOperation: Operation;
  updateOperationMemberRole: Operation;
  updateOwnProfile: User;
  updateTask: Task;
  updateUser: User;
  updateWikiDocument: WikiDocument;
};


export type MutationAddCredentialCommentArgs = {
  credentialId: Scalars['ID']['input'];
  text: Scalars['String']['input'];
};


export type MutationAddOperationMemberArgs = {
  operationId: Scalars['ID']['input'];
  role: OperationRole;
  userId: Scalars['ID']['input'];
};


export type MutationAddTaskWikiReferenceArgs = {
  taskId: Scalars['ID']['input'];
  wikiId: Scalars['ID']['input'];
};


export type MutationAdminRevokeAllUserSessionsArgs = {
  userId: Scalars['ID']['input'];
};


export type MutationAdminRevokeSessionArgs = {
  id: Scalars['ID']['input'];
};


export type MutationBulkImportHashesArgs = {
  input: BulkImportHashesInput;
  operationId: Scalars['ID']['input'];
};


export type MutationChangeTaskStageArgs = {
  input: ChangeTaskStageInput;
};


export type MutationCompleteGuideArgs = {
  guide: Scalars['String']['input'];
};


export type MutationCreateAgentKeyArgs = {
  input: CreateAgentKeyInput;
};


export type MutationCreateCredentialArgs = {
  input: CreateCredentialInput;
  operationId: Scalars['ID']['input'];
};


export type MutationCreateCustomTimelineEventArgs = {
  input: CreateCustomTimelineEventInput;
  operationId: Scalars['ID']['input'];
};


export type MutationCreateHashArgs = {
  input: CreateHashInput;
  operationId: Scalars['ID']['input'];
};


export type MutationCreateHostArgs = {
  input: CreateHostInput;
  operationId: Scalars['ID']['input'];
};


export type MutationCreateOperationArgs = {
  input: CreateOperationInput;
};


export type MutationCreateTaskArgs = {
  input: CreateTaskInput;
};


export type MutationCreateUserArgs = {
  input: CreateUserInput;
};


export type MutationCreateWikiDocumentArgs = {
  input: CreateWikiDocumentInput;
  operationId: Scalars['ID']['input'];
};


export type MutationCreateWikiDocumentBackupArgs = {
  description?: InputMaybe<Scalars['String']['input']>;
  documentId: Scalars['ID']['input'];
};


export type MutationDeleteAgentKeyArgs = {
  id: Scalars['ID']['input'];
};


export type MutationDeleteCredentialArgs = {
  id: Scalars['ID']['input'];
};


export type MutationDeleteCredentialCommentArgs = {
  commentId: Scalars['ID']['input'];
  credentialId: Scalars['ID']['input'];
};


export type MutationDeleteCustomTimelineEventArgs = {
  id: Scalars['ID']['input'];
};


export type MutationDeleteHashArgs = {
  id: Scalars['ID']['input'];
};


export type MutationDeleteHostArgs = {
  id: Scalars['ID']['input'];
};


export type MutationDeleteOperationArgs = {
  id: Scalars['ID']['input'];
};


export type MutationDeleteTaskArgs = {
  id: Scalars['ID']['input'];
};


export type MutationDeleteUserArgs = {
  id: Scalars['ID']['input'];
};


export type MutationDeleteWikiDocumentArgs = {
  id: Scalars['ID']['input'];
};


export type MutationDeleteWikiDocumentBackupArgs = {
  id: Scalars['ID']['input'];
};


export type MutationDuplicateWikiDocumentArgs = {
  id: Scalars['ID']['input'];
  withChildren?: InputMaybe<Scalars['Boolean']['input']>;
};


export type MutationEmptyWikiDocumentTrashArgs = {
  operationId: Scalars['ID']['input'];
};


export type MutationImportLocalPreferencesArgs = {
  input: ImportLocalPreferencesInput;
};


export type MutationInstantiateTemplateArgs = {
  color?: InputMaybe<Scalars['String']['input']>;
  emoji?: InputMaybe<Scalars['String']['input']>;
  icon?: InputMaybe<Scalars['String']['input']>;
  parentDocumentId?: InputMaybe<Scalars['ID']['input']>;
  targetOperationId: Scalars['ID']['input'];
  templateId: Scalars['ID']['input'];
  title?: InputMaybe<Scalars['String']['input']>;
};


export type MutationMarkHashCrackedArgs = {
  id: Scalars['ID']['input'];
  input: MarkHashCrackedInput;
};


export type MutationPermanentlyDeleteWikiDocumentArgs = {
  id: Scalars['ID']['input'];
};


export type MutationPublishOperatorFocusArgs = {
  input: OperatorFocusInput;
};


export type MutationPurgeTaskArgs = {
  id: Scalars['ID']['input'];
};


export type MutationRecordIconUseArgs = {
  name: Scalars['String']['input'];
};


export type MutationRegenerateAgentKeyArgs = {
  id: Scalars['ID']['input'];
};


export type MutationRemoveModuleArgs = {
  instance: Scalars['ID']['input'];
};


export type MutationRemoveOperationMemberArgs = {
  operationId: Scalars['ID']['input'];
  userId: Scalars['ID']['input'];
};


export type MutationRemoveSkillArgs = {
  name: Scalars['String']['input'];
};


export type MutationReorderWikiDocumentSiblingsArgs = {
  input: ReorderWikiDocumentSiblingsInput;
};


export type MutationRestoreTaskArgs = {
  id: Scalars['ID']['input'];
};


export type MutationRestoreWikiDocumentArgs = {
  cascade?: InputMaybe<Scalars['Boolean']['input']>;
  id: Scalars['ID']['input'];
};


export type MutationRestoreWikiDocumentBackupArgs = {
  backupId: Scalars['ID']['input'];
  documentId: Scalars['ID']['input'];
};


export type MutationRevokeSessionArgs = {
  id: Scalars['ID']['input'];
};


export type MutationSetAgentKeyEnabledArgs = {
  enabled: Scalars['Boolean']['input'];
  id: Scalars['ID']['input'];
};


export type MutationSetHiddenIdentitiesArgs = {
  names: Array<Scalars['String']['input']>;
};


export type MutationSetMyApiKeyEnabledArgs = {
  enabled: Scalars['Boolean']['input'];
};


export type MutationSetTaskAssigneesArgs = {
  assigneeIds: Array<Scalars['ID']['input']>;
  taskId: Scalars['ID']['input'];
};


export type MutationSetTaskCredentialReferencesArgs = {
  credentialIds: Array<Scalars['ID']['input']>;
  taskId: Scalars['ID']['input'];
};


export type MutationSetTaskWikiReferencesArgs = {
  taskId: Scalars['ID']['input'];
  wikiIds: Array<Scalars['ID']['input']>;
};


export type MutationSetWikiDocumentTemplateArgs = {
  id: Scalars['ID']['input'];
  isTemplate: Scalars['Boolean']['input'];
};


export type MutationSnoozeSkillArgs = {
  name: Scalars['String']['input'];
  version: Scalars['Int']['input'];
};


export type MutationSnoozeSkillUpdateArgs = {
  version: Scalars['Int']['input'];
};


export type MutationTouchRecentOperationArgs = {
  operationId: Scalars['ID']['input'];
};


export type MutationTrackWikiDocumentVisitArgs = {
  documentId: Scalars['ID']['input'];
};


export type MutationTransferSkillArgs = {
  name: Scalars['String']['input'];
  userId: Scalars['ID']['input'];
};


export type MutationUpdateAgentKeyArgs = {
  id: Scalars['ID']['input'];
  input: UpdateAgentKeyInput;
};


export type MutationUpdateCredentialArgs = {
  id: Scalars['ID']['input'];
  input: UpdateCredentialInput;
};


export type MutationUpdateCredentialCommentArgs = {
  commentId: Scalars['ID']['input'];
  credentialId: Scalars['ID']['input'];
  text: Scalars['String']['input'];
};


export type MutationUpdateCustomTimelineEventArgs = {
  id: Scalars['ID']['input'];
  input: UpdateCustomTimelineEventInput;
};


export type MutationUpdateHashArgs = {
  id: Scalars['ID']['input'];
  input: UpdateHashInput;
};


export type MutationUpdateHostArgs = {
  id: Scalars['ID']['input'];
  input: UpdateHostInput;
};


export type MutationUpdateOperationArgs = {
  id: Scalars['ID']['input'];
  input: UpdateOperationInput;
};


export type MutationUpdateOperationMemberRoleArgs = {
  operationId: Scalars['ID']['input'];
  role: OperationRole;
  userId: Scalars['ID']['input'];
};


export type MutationUpdateOwnProfileArgs = {
  input: UpdateUserInput;
};


export type MutationUpdateTaskArgs = {
  id: Scalars['ID']['input'];
  input: UpdateTaskInput;
};


export type MutationUpdateUserArgs = {
  id: Scalars['ID']['input'];
  input: UpdateUserInput;
};


export type MutationUpdateWikiDocumentArgs = {
  id: Scalars['ID']['input'];
  input: UpdateWikiDocumentInput;
};

export type NetworkInterface = {
  addresses: Array<Scalars['String']['output']>;
  mac: Scalars['String']['output'];
  name: Scalars['String']['output'];
};

export type NetworkInterfaceInput = {
  addresses?: InputMaybe<Array<Scalars['String']['input']>>;
  mac?: InputMaybe<Scalars['String']['input']>;
  name: Scalars['String']['input'];
};

export type Operation = {
  createdAt: Scalars['String']['output'];
  description: Scalars['String']['output'];
  id: Scalars['ID']['output'];
  members: Array<OperationMember>;
  name: Scalars['String']['output'];
  updatedAt: Scalars['String']['output'];
};

export type OperationConnection = {
  edges: Array<OperationEdge>;
  pageInfo: PageInfo;
  totalCount: Scalars['Int']['output'];
};

export type OperationEdge = {
  cursor: Scalars['String']['output'];
  node: Operation;
};

export type OperationEvent = {
  action: EventAction;
  name?: Maybe<Scalars['String']['output']>;
  operation?: Maybe<Operation>;
  operationId: Scalars['ID']['output'];
};

export type OperationMember = {
  role: OperationRole;
  user: User;
};

export type OperationMemberEvent = {
  action: EventAction;
  operationId: Scalars['ID']['output'];
  userId: Scalars['ID']['output'];
};

export type OperationRole =
  | 'ADMIN'
  | 'OPERATOR'
  | 'VIEWER';

export type OperationSortField =
  | 'CREATED_AT'
  | 'NAME';

export type OperatorFocus = {
  credentialId?: Maybe<Scalars['ID']['output']>;
  findingsTab?: Maybe<Scalars['String']['output']>;
  hashId?: Maybe<Scalars['ID']['output']>;
  hostId?: Maybe<Scalars['ID']['output']>;
  operationId?: Maybe<Scalars['ID']['output']>;
  route: Scalars['String']['output'];
  searchSummary?: Maybe<Scalars['String']['output']>;
  taskId?: Maybe<Scalars['ID']['output']>;
  topologyFocusedEdgeId?: Maybe<Scalars['String']['output']>;
  topologyFocusedNodeId?: Maybe<Scalars['String']['output']>;
  topologyLens?: Maybe<Scalars['String']['output']>;
  updatedAt?: Maybe<Scalars['String']['output']>;
  wikiDocumentId?: Maybe<Scalars['ID']['output']>;
  wikiOperationId?: Maybe<Scalars['ID']['output']>;
};

export type OperatorFocusInput = {
  credentialId?: InputMaybe<Scalars['ID']['input']>;
  findingsTab?: InputMaybe<Scalars['String']['input']>;
  hashId?: InputMaybe<Scalars['ID']['input']>;
  hostId?: InputMaybe<Scalars['ID']['input']>;
  operationId?: InputMaybe<Scalars['ID']['input']>;
  route: Scalars['String']['input'];
  searchSummary?: InputMaybe<Scalars['String']['input']>;
  taskId?: InputMaybe<Scalars['ID']['input']>;
  topologyFocusedEdgeId?: InputMaybe<Scalars['String']['input']>;
  topologyFocusedNodeId?: InputMaybe<Scalars['String']['input']>;
  topologyLens?: InputMaybe<Scalars['String']['input']>;
  wikiDocumentId?: InputMaybe<Scalars['ID']['input']>;
  wikiOperationId?: InputMaybe<Scalars['ID']['input']>;
};

export type PageInfo = {
  endCursor?: Maybe<Scalars['String']['output']>;
  hasNextPage: Scalars['Boolean']['output'];
  hasPreviousPage: Scalars['Boolean']['output'];
  startCursor?: Maybe<Scalars['String']['output']>;
};

export type PresenceAction =
  | 'JOINED'
  | 'LEFT';

export type Query = {
  credential: Credential;
  credentialTags: Array<Scalars['String']['output']>;
  credentials: CredentialConnection;
  hash: Hash;
  hashTags: Array<Scalars['String']['output']>;
  hashes: HashConnection;
  host: Host;
  hosts: HostConnection;
  me: User;
  modules: Array<Module>;
  myAPIKey?: Maybe<ApiKey>;
  myAgentActions: AgentActionConnection;
  myAgentActivitySummary: Array<AgentActivitySummary>;
  myAgentKeys: Array<AgentKey>;
  myCredentialTags: Array<Scalars['String']['output']>;
  myCredentials: CredentialConnection;
  myHashTags: Array<Scalars['String']['output']>;
  myHashes: HashConnection;
  myOperationRole?: Maybe<OperationRole>;
  myOperatorFocus?: Maybe<OperatorFocus>;
  mySessions: SessionConnection;
  operation: Operation;
  operations: OperationConnection;
  session: Session;
  sessions: SessionConnection;
  skillChangelog: SkillChangelog;
  skillRegistry: SkillRegistry;
  skillVersions: Array<SkillVersion>;
  task: Task;
  taskTrash: TaskConnection;
  tasks: TaskConnection;
  tasksReferencingCredential: Array<Task>;
  tasksReferencingWikiDocument: Array<Task>;
  timelineBuckets: Array<TimelineBucket>;
  timelineEventsByDay: TimelineEventConnection;
  user: User;
  userSuggestions: Array<UserSuggestion>;
  users: UserConnection;
  wikiDocument: WikiDocument;
  wikiDocumentBacklinks: Array<WikiDocument>;
  wikiDocumentBackup: WikiDocumentBackup;
  wikiDocumentBackups: WikiDocumentBackupConnection;
  wikiDocumentChildren: Array<WikiDocument>;
  wikiDocumentDescendantIds: Array<Scalars['ID']['output']>;
  wikiDocumentHistory: WikiDocumentVisitConnection;
  wikiDocumentMarkdown: Scalars['String']['output'];
  wikiDocumentPageTypes: Array<WikiPageTypeCount>;
  wikiDocumentPresence: WikiDocumentPresence;
  wikiDocumentTags: Array<WikiTagCount>;
  wikiDocumentTrash: WikiDocumentConnection;
  wikiDocumentTrashCount: Scalars['Int']['output'];
  wikiDocumentTrashedDescendants: Array<WikiDocument>;
  wikiDocumentTree: Array<WikiDocument>;
  wikiDocumentTreeRevealPath: Array<WikiDocument>;
  wikiDocuments: WikiDocumentConnection;
  wikiDocumentsReferencingCredential: Array<WikiDocument>;
  wikiDocumentsReferencingHash: Array<WikiDocument>;
  wikiDrawingScene: Scalars['String']['output'];
  wikiOperationPresence: Array<WikiDocumentPresence>;
  wikiSearch: WikiSearchConnection;
  wikiTemplates: Array<WikiDocument>;
};


export type QueryCredentialArgs = {
  id: Scalars['ID']['input'];
};


export type QueryCredentialTagsArgs = {
  operationId: Scalars['ID']['input'];
};


export type QueryCredentialsArgs = {
  after?: InputMaybe<Scalars['String']['input']>;
  before?: InputMaybe<Scalars['String']['input']>;
  first?: InputMaybe<Scalars['Int']['input']>;
  last?: InputMaybe<Scalars['Int']['input']>;
  operationId: Scalars['ID']['input'];
  search?: InputMaybe<Scalars['String']['input']>;
  searchFields?: InputMaybe<Array<CredentialSearchField>>;
  sortBy?: InputMaybe<CredentialSortField>;
  sortDirection?: InputMaybe<SortDirection>;
  tags?: InputMaybe<Array<Scalars['String']['input']>>;
  type?: InputMaybe<CredentialType>;
  validity?: InputMaybe<Array<CredentialValidity>>;
};


export type QueryHashArgs = {
  id: Scalars['ID']['input'];
};


export type QueryHashTagsArgs = {
  operationId: Scalars['ID']['input'];
};


export type QueryHashesArgs = {
  after?: InputMaybe<Scalars['String']['input']>;
  before?: InputMaybe<Scalars['String']['input']>;
  first?: InputMaybe<Scalars['Int']['input']>;
  hasCredential?: InputMaybe<Scalars['Boolean']['input']>;
  last?: InputMaybe<Scalars['Int']['input']>;
  operationId: Scalars['ID']['input'];
  search?: InputMaybe<Scalars['String']['input']>;
  statuses?: InputMaybe<Array<HashStatus>>;
  tags?: InputMaybe<Array<Scalars['String']['input']>>;
};


export type QueryHostArgs = {
  id: Scalars['ID']['input'];
};


export type QueryHostsArgs = {
  after?: InputMaybe<Scalars['String']['input']>;
  before?: InputMaybe<Scalars['String']['input']>;
  first?: InputMaybe<Scalars['Int']['input']>;
  last?: InputMaybe<Scalars['Int']['input']>;
  operationId: Scalars['ID']['input'];
  search?: InputMaybe<Scalars['String']['input']>;
  sortBy?: InputMaybe<HostSortField>;
  sortDirection?: InputMaybe<SortDirection>;
};


export type QueryModulesArgs = {
  status?: InputMaybe<Array<Scalars['String']['input']>>;
};


export type QueryMyAgentActionsArgs = {
  after?: InputMaybe<Scalars['String']['input']>;
  agentKeyId?: InputMaybe<Scalars['ID']['input']>;
  before?: InputMaybe<Scalars['String']['input']>;
  first?: InputMaybe<Scalars['Int']['input']>;
  last?: InputMaybe<Scalars['Int']['input']>;
  operationId?: InputMaybe<Scalars['ID']['input']>;
  outcomes?: InputMaybe<Array<AgentActionOutcome>>;
  writesOnly?: InputMaybe<Scalars['Boolean']['input']>;
};


export type QueryMyCredentialTagsArgs = {
  operationIds?: InputMaybe<Array<Scalars['ID']['input']>>;
};


export type QueryMyCredentialsArgs = {
  after?: InputMaybe<Scalars['String']['input']>;
  before?: InputMaybe<Scalars['String']['input']>;
  first?: InputMaybe<Scalars['Int']['input']>;
  last?: InputMaybe<Scalars['Int']['input']>;
  operationIds?: InputMaybe<Array<Scalars['ID']['input']>>;
  search?: InputMaybe<Scalars['String']['input']>;
  searchFields?: InputMaybe<Array<CredentialSearchField>>;
  sortBy?: InputMaybe<CredentialSortField>;
  sortDirection?: InputMaybe<SortDirection>;
  tags?: InputMaybe<Array<Scalars['String']['input']>>;
  type?: InputMaybe<CredentialType>;
  validity?: InputMaybe<Array<CredentialValidity>>;
};


export type QueryMyHashTagsArgs = {
  operationIds?: InputMaybe<Array<Scalars['ID']['input']>>;
};


export type QueryMyHashesArgs = {
  after?: InputMaybe<Scalars['String']['input']>;
  before?: InputMaybe<Scalars['String']['input']>;
  first?: InputMaybe<Scalars['Int']['input']>;
  hasCredential?: InputMaybe<Scalars['Boolean']['input']>;
  last?: InputMaybe<Scalars['Int']['input']>;
  operationIds?: InputMaybe<Array<Scalars['ID']['input']>>;
  search?: InputMaybe<Scalars['String']['input']>;
  statuses?: InputMaybe<Array<HashStatus>>;
  tags?: InputMaybe<Array<Scalars['String']['input']>>;
};


export type QueryMyOperationRoleArgs = {
  operationId: Scalars['ID']['input'];
};


export type QueryMySessionsArgs = {
  activeOnly?: InputMaybe<Scalars['Boolean']['input']>;
  after?: InputMaybe<Scalars['String']['input']>;
  before?: InputMaybe<Scalars['String']['input']>;
  first?: InputMaybe<Scalars['Int']['input']>;
  last?: InputMaybe<Scalars['Int']['input']>;
};


export type QueryOperationArgs = {
  id: Scalars['ID']['input'];
};


export type QueryOperationsArgs = {
  after?: InputMaybe<Scalars['String']['input']>;
  before?: InputMaybe<Scalars['String']['input']>;
  first?: InputMaybe<Scalars['Int']['input']>;
  last?: InputMaybe<Scalars['Int']['input']>;
  search?: InputMaybe<Scalars['String']['input']>;
  sortBy?: InputMaybe<OperationSortField>;
  sortDirection?: InputMaybe<SortDirection>;
};


export type QuerySessionArgs = {
  id: Scalars['ID']['input'];
};


export type QuerySessionsArgs = {
  activeOnly?: InputMaybe<Scalars['Boolean']['input']>;
  after?: InputMaybe<Scalars['String']['input']>;
  before?: InputMaybe<Scalars['String']['input']>;
  first?: InputMaybe<Scalars['Int']['input']>;
  last?: InputMaybe<Scalars['Int']['input']>;
  search?: InputMaybe<Scalars['String']['input']>;
  userId?: InputMaybe<Scalars['ID']['input']>;
};


export type QuerySkillVersionsArgs = {
  name: Scalars['String']['input'];
};


export type QueryTaskArgs = {
  id: Scalars['ID']['input'];
};


export type QueryTaskTrashArgs = {
  after?: InputMaybe<Scalars['String']['input']>;
  before?: InputMaybe<Scalars['String']['input']>;
  first?: InputMaybe<Scalars['Int']['input']>;
  last?: InputMaybe<Scalars['Int']['input']>;
  operationId: Scalars['ID']['input'];
};


export type QueryTasksArgs = {
  after?: InputMaybe<Scalars['String']['input']>;
  before?: InputMaybe<Scalars['String']['input']>;
  excludeStages?: InputMaybe<Array<TaskStage>>;
  first?: InputMaybe<Scalars['Int']['input']>;
  last?: InputMaybe<Scalars['Int']['input']>;
  operationId: Scalars['ID']['input'];
  profitScoreMax?: InputMaybe<Scalars['Int']['input']>;
  profitScoreMin?: InputMaybe<Scalars['Int']['input']>;
  riskScoreMax?: InputMaybe<Scalars['Int']['input']>;
  riskScoreMin?: InputMaybe<Scalars['Int']['input']>;
  search?: InputMaybe<Scalars['String']['input']>;
  stage?: InputMaybe<TaskStage>;
};


export type QueryTasksReferencingCredentialArgs = {
  credentialId: Scalars['ID']['input'];
};


export type QueryTasksReferencingWikiDocumentArgs = {
  documentId: Scalars['ID']['input'];
};


export type QueryTimelineBucketsArgs = {
  actorIds?: InputMaybe<Array<Scalars['ID']['input']>>;
  from?: InputMaybe<Scalars['String']['input']>;
  granularity?: InputMaybe<TimelineGranularity>;
  operationId: Scalars['ID']['input'];
  timezone: Scalars['String']['input'];
  to?: InputMaybe<Scalars['String']['input']>;
  types?: InputMaybe<Array<Scalars['String']['input']>>;
};


export type QueryTimelineEventsByDayArgs = {
  actorIds?: InputMaybe<Array<Scalars['ID']['input']>>;
  after?: InputMaybe<Scalars['String']['input']>;
  date: Scalars['String']['input'];
  first?: InputMaybe<Scalars['Int']['input']>;
  granularity?: InputMaybe<TimelineGranularity>;
  operationId: Scalars['ID']['input'];
  timezone: Scalars['String']['input'];
  types?: InputMaybe<Array<Scalars['String']['input']>>;
};


export type QueryUserArgs = {
  id: Scalars['ID']['input'];
};


export type QueryUserSuggestionsArgs = {
  first?: InputMaybe<Scalars['Int']['input']>;
  search: Scalars['String']['input'];
};


export type QueryUsersArgs = {
  after?: InputMaybe<Scalars['String']['input']>;
  before?: InputMaybe<Scalars['String']['input']>;
  first?: InputMaybe<Scalars['Int']['input']>;
  last?: InputMaybe<Scalars['Int']['input']>;
  search?: InputMaybe<Scalars['String']['input']>;
  sortBy?: InputMaybe<UserSortField>;
  sortDirection?: InputMaybe<SortDirection>;
};


export type QueryWikiDocumentArgs = {
  id: Scalars['ID']['input'];
};


export type QueryWikiDocumentBacklinksArgs = {
  documentId: Scalars['ID']['input'];
};


export type QueryWikiDocumentBackupArgs = {
  id: Scalars['ID']['input'];
};


export type QueryWikiDocumentBackupsArgs = {
  after?: InputMaybe<Scalars['String']['input']>;
  before?: InputMaybe<Scalars['String']['input']>;
  documentId: Scalars['ID']['input'];
  first?: InputMaybe<Scalars['Int']['input']>;
  last?: InputMaybe<Scalars['Int']['input']>;
  trigger?: InputMaybe<WikiDocumentBackupTrigger>;
};


export type QueryWikiDocumentChildrenArgs = {
  operationId: Scalars['ID']['input'];
  parentDocumentId?: InputMaybe<Scalars['ID']['input']>;
};


export type QueryWikiDocumentDescendantIdsArgs = {
  documentId: Scalars['ID']['input'];
};


export type QueryWikiDocumentHistoryArgs = {
  limit?: InputMaybe<Scalars['Int']['input']>;
  offset?: InputMaybe<Scalars['Int']['input']>;
  operationId: Scalars['ID']['input'];
};


export type QueryWikiDocumentMarkdownArgs = {
  id: Scalars['ID']['input'];
};


export type QueryWikiDocumentPageTypesArgs = {
  operationId: Scalars['ID']['input'];
};


export type QueryWikiDocumentPresenceArgs = {
  documentId: Scalars['ID']['input'];
};


export type QueryWikiDocumentTagsArgs = {
  operationId: Scalars['ID']['input'];
};


export type QueryWikiDocumentTrashArgs = {
  after?: InputMaybe<Scalars['String']['input']>;
  before?: InputMaybe<Scalars['String']['input']>;
  first?: InputMaybe<Scalars['Int']['input']>;
  last?: InputMaybe<Scalars['Int']['input']>;
  operationId: Scalars['ID']['input'];
};


export type QueryWikiDocumentTrashCountArgs = {
  operationId: Scalars['ID']['input'];
};


export type QueryWikiDocumentTrashedDescendantsArgs = {
  documentId: Scalars['ID']['input'];
};


export type QueryWikiDocumentTreeArgs = {
  operationId: Scalars['ID']['input'];
};


export type QueryWikiDocumentTreeRevealPathArgs = {
  documentId: Scalars['ID']['input'];
};


export type QueryWikiDocumentsArgs = {
  after?: InputMaybe<Scalars['String']['input']>;
  before?: InputMaybe<Scalars['String']['input']>;
  first?: InputMaybe<Scalars['Int']['input']>;
  last?: InputMaybe<Scalars['Int']['input']>;
  operationId: Scalars['ID']['input'];
  parentDocumentId?: InputMaybe<Scalars['ID']['input']>;
  search?: InputMaybe<Scalars['String']['input']>;
  sort?: InputMaybe<WikiDocumentSort>;
};


export type QueryWikiDocumentsReferencingCredentialArgs = {
  credentialId: Scalars['ID']['input'];
};


export type QueryWikiDocumentsReferencingHashArgs = {
  hashId: Scalars['ID']['input'];
};


export type QueryWikiDrawingSceneArgs = {
  id: Scalars['ID']['input'];
};


export type QueryWikiOperationPresenceArgs = {
  operationId: Scalars['ID']['input'];
};


export type QueryWikiSearchArgs = {
  limit?: InputMaybe<Scalars['Int']['input']>;
  offset?: InputMaybe<Scalars['Int']['input']>;
  operationId: Scalars['ID']['input'];
  query: Scalars['String']['input'];
  scope?: InputMaybe<Scalars['ID']['input']>;
};


export type QueryWikiTemplatesArgs = {
  operationId: Scalars['ID']['input'];
};

export type ReorderWikiDocumentSiblingsInput = {
  operationId: Scalars['ID']['input'];
  orderedIds: Array<Scalars['ID']['input']>;
  parentDocumentId?: InputMaybe<Scalars['ID']['input']>;
};

export type Route = {
  destination: Scalars['String']['output'];
  gateway: Scalars['String']['output'];
  interface: Scalars['String']['output'];
};

export type RouteInput = {
  destination: Scalars['String']['input'];
  gateway?: InputMaybe<Scalars['String']['input']>;
  interface?: InputMaybe<Scalars['String']['input']>;
};

export type Session = {
  browser: Scalars['String']['output'];
  createdAt: Scalars['String']['output'];
  device: Scalars['String']['output'];
  id: Scalars['ID']['output'];
  ipAddress: Scalars['String']['output'];
  isCurrent: Scalars['Boolean']['output'];
  lastActivityAt?: Maybe<Scalars['String']['output']>;
  os: Scalars['String']['output'];
  status: SessionStatus;
  updatedAt: Scalars['String']['output'];
  user: User;
  userAgent: Scalars['String']['output'];
  userId: Scalars['ID']['output'];
};

export type SessionConnection = {
  edges: Array<SessionEdge>;
  pageInfo: PageInfo;
  totalCount: Scalars['Int']['output'];
};

export type SessionEdge = {
  cursor: Scalars['String']['output'];
  node: Session;
};

export type SessionEvent = {
  action: EventAction;
  session?: Maybe<Session>;
  sessionId: Scalars['ID']['output'];
  userId: Scalars['ID']['output'];
};

export type SessionStatus =
  | 'ACTIVE'
  | 'INACTIVE';

export type Skill = {
  currentVersion: Scalars['Int']['output'];
  description: Scalars['String']['output'];
  downloadUrl: Scalars['String']['output'];
  downloadedAt?: Maybe<Scalars['String']['output']>;
  downloadedVersion?: Maybe<Scalars['Int']['output']>;
  id: Scalars['ID']['output'];
  mine: Scalars['Boolean']['output'];
  name: Scalars['String']['output'];
  ownerUserId: Scalars['ID']['output'];
  ownerUsername: Scalars['String']['output'];
  sizeBytes: Scalars['Int']['output'];
  snoozedVersion?: Maybe<Scalars['Int']['output']>;
  updatedAt: Scalars['String']['output'];
};

export type SkillChangelog = {
  currentVersion: Scalars['Int']['output'];
  releases: Array<SkillRelease>;
};

export type SkillEvent = {
  action: EventAction;
  name: Scalars['String']['output'];
  skillId: Scalars['ID']['output'];
};

export type SkillRegistry = {
  maxUploadBytes: Scalars['Int']['output'];
  skills: Array<Skill>;
};

export type SkillRelease = {
  date: Scalars['String']['output'];
  notes: Array<Scalars['String']['output']>;
  version: Scalars['Int']['output'];
};

export type SkillVersion = {
  notes: Scalars['String']['output'];
  sizeBytes: Scalars['Int']['output'];
  uploadedAt: Scalars['String']['output'];
  uploadedByUsername: Scalars['String']['output'];
  version: Scalars['Int']['output'];
  viaAgent: Scalars['Boolean']['output'];
};

export type SortDirection =
  | 'ASC'
  | 'DESC';

export type Subscription = {
  agentActivity: AgentActivityEvent;
  credentialChanged: CredentialEvent;
  hashChanged: HashEvent;
  hostChanged: HostEvent;
  moduleChanged: ModuleEvent;
  myAgentActionOccurred: AgentActivityEvent;
  myCredentialChanged: CredentialEvent;
  myHashChanged: HashEvent;
  mySessionChanged: SessionEvent;
  operationChanged: OperationEvent;
  operationMemberChanged: OperationMemberEvent;
  sessionChanged: SessionEvent;
  skillChanged: SkillEvent;
  taskChanged: TaskEvent;
  timelineEventAdded: TimelineEvent;
  userChanged: UserEvent;
  wikiDocumentChanged: WikiDocumentEvent;
  wikiDocumentPresenceChanged: WikiDocumentPresenceEvent;
};


export type SubscriptionAgentActivityArgs = {
  operationId: Scalars['ID']['input'];
};


export type SubscriptionCredentialChangedArgs = {
  operationId: Scalars['ID']['input'];
};


export type SubscriptionHashChangedArgs = {
  operationId: Scalars['ID']['input'];
};


export type SubscriptionHostChangedArgs = {
  operationId: Scalars['ID']['input'];
};


export type SubscriptionMyCredentialChangedArgs = {
  operationIds?: InputMaybe<Array<Scalars['ID']['input']>>;
};


export type SubscriptionMyHashChangedArgs = {
  operationIds?: InputMaybe<Array<Scalars['ID']['input']>>;
};


export type SubscriptionOperationChangedArgs = {
  operationId?: InputMaybe<Scalars['ID']['input']>;
};


export type SubscriptionOperationMemberChangedArgs = {
  operationId?: InputMaybe<Scalars['ID']['input']>;
};


export type SubscriptionSessionChangedArgs = {
  userId?: InputMaybe<Scalars['ID']['input']>;
};


export type SubscriptionTaskChangedArgs = {
  operationId: Scalars['ID']['input'];
};


export type SubscriptionTimelineEventAddedArgs = {
  operationId: Scalars['ID']['input'];
};


export type SubscriptionWikiDocumentChangedArgs = {
  operationId: Scalars['ID']['input'];
};


export type SubscriptionWikiDocumentPresenceChangedArgs = {
  operationId: Scalars['ID']['input'];
};

export type Task = {
  assignees: Array<User>;
  createdAt: Scalars['String']['output'];
  createdBy?: Maybe<User>;
  credentialReferences: Array<Credential>;
  deletedAt?: Maybe<Scalars['String']['output']>;
  description: Scalars['String']['output'];
  doneAt?: Maybe<Scalars['String']['output']>;
  id: Scalars['ID']['output'];
  lastUpdatedAt?: Maybe<Scalars['String']['output']>;
  lastUpdatedBy?: Maybe<User>;
  name: Scalars['String']['output'];
  operation: Operation;
  operationId: Scalars['ID']['output'];
  profitDescription: Scalars['String']['output'];
  profitScore: Scalars['Int']['output'];
  riskDescription: Scalars['String']['output'];
  riskScore: Scalars['Int']['output'];
  stage: TaskStage;
  status: TaskStatus;
  summary: Scalars['String']['output'];
  updatedAt: Scalars['String']['output'];
  wikiReferences: Array<WikiDocument>;
};

export type TaskConnection = {
  edges: Array<TaskEdge>;
  pageInfo: PageInfo;
  totalCount: Scalars['Int']['output'];
};

export type TaskEdge = {
  cursor: Scalars['String']['output'];
  node: Task;
};

export type TaskEvent = {
  action: EventAction;
  operationId: Scalars['ID']['output'];
  task?: Maybe<Task>;
  taskId: Scalars['ID']['output'];
};

export type TaskStage =
  | 'BACKLOG'
  | 'DONE'
  | 'IN_PROCESS'
  | 'TODO';

export type TaskStatus =
  | 'FAIL'
  | 'SUCCESS'
  | 'UNDEFINED';

export type TimelineBucket = {
  bucketStart: Scalars['String']['output'];
  count: Scalars['Int']['output'];
  topicCounts: Array<TimelineTopicCount>;
};

export type TimelineEvent = {
  actor?: Maybe<User>;
  actorKind: Scalars['String']['output'];
  actorLabel: Scalars['String']['output'];
  id: Scalars['ID']['output'];
  metadata: Scalars['String']['output'];
  occurredAt: Scalars['String']['output'];
  operationId: Scalars['ID']['output'];
  subjectId: Scalars['ID']['output'];
  subjectKind: Scalars['String']['output'];
  subjectName: Scalars['String']['output'];
  topic: Scalars['String']['output'];
};

export type TimelineEventConnection = {
  edges: Array<TimelineEventEdge>;
  pageInfo: PageInfo;
};

export type TimelineEventEdge = {
  cursor: Scalars['String']['output'];
  node: TimelineEvent;
};

export type TimelineGranularity =
  | 'DAY'
  | 'MONTH'
  | 'WEEK';

export type TimelineTopicCount = {
  color: Scalars['String']['output'];
  count: Scalars['Int']['output'];
  emoji: Scalars['String']['output'];
  icon: Scalars['String']['output'];
  subjectKind: Scalars['String']['output'];
  topic: Scalars['String']['output'];
};

export type UpdateAgentKeyInput = {
  allowWrites?: InputMaybe<Scalars['Boolean']['input']>;
  maxRole?: InputMaybe<OperationRole>;
  name?: InputMaybe<Scalars['String']['input']>;
  operationScopes?: InputMaybe<Array<Scalars['ID']['input']>>;
};

export type UpdateCredentialInput = {
  keys?: InputMaybe<Array<CredentialKeyInput>>;
  name?: InputMaybe<Scalars['String']['input']>;
  password?: InputMaybe<Scalars['String']['input']>;
  properties?: InputMaybe<Array<CredentialPropertyInput>>;
  tags?: InputMaybe<Array<Scalars['String']['input']>>;
  type?: InputMaybe<CredentialType>;
  username?: InputMaybe<Scalars['String']['input']>;
  validity?: InputMaybe<CredentialValidity>;
};

export type UpdateCustomTimelineEventInput = {
  color?: InputMaybe<Scalars['String']['input']>;
  description?: InputMaybe<Scalars['String']['input']>;
  emoji?: InputMaybe<Scalars['String']['input']>;
  icon?: InputMaybe<Scalars['String']['input']>;
  name?: InputMaybe<Scalars['String']['input']>;
  occurredAt?: InputMaybe<Scalars['String']['input']>;
};

export type UpdateHashInput = {
  comment?: InputMaybe<Scalars['String']['input']>;
  credentialId?: InputMaybe<Scalars['ID']['input']>;
  status?: InputMaybe<HashStatus>;
  tags?: InputMaybe<Array<Scalars['String']['input']>>;
  value?: InputMaybe<Scalars['String']['input']>;
};

export type UpdateHostInput = {
  color?: InputMaybe<Scalars['String']['input']>;
  description?: InputMaybe<Scalars['String']['input']>;
  emoji?: InputMaybe<Scalars['String']['input']>;
  hostname?: InputMaybe<Scalars['String']['input']>;
  icon?: InputMaybe<Scalars['String']['input']>;
  interfaces?: InputMaybe<Array<NetworkInterfaceInput>>;
  logins?: InputMaybe<Array<LoginInput>>;
  os?: InputMaybe<Scalars['String']['input']>;
  routes?: InputMaybe<Array<RouteInput>>;
};

export type UpdateOperationInput = {
  description?: InputMaybe<Scalars['String']['input']>;
  name?: InputMaybe<Scalars['String']['input']>;
};

export type UpdateTaskInput = {
  description?: InputMaybe<Scalars['String']['input']>;
  name?: InputMaybe<Scalars['String']['input']>;
  profitDescription?: InputMaybe<Scalars['String']['input']>;
  profitScore?: InputMaybe<Scalars['Int']['input']>;
  riskDescription?: InputMaybe<Scalars['String']['input']>;
  riskScore?: InputMaybe<Scalars['Int']['input']>;
};

export type UpdateUserInput = {
  active?: InputMaybe<Scalars['Boolean']['input']>;
  password?: InputMaybe<Scalars['String']['input']>;
  roles?: InputMaybe<Array<InputMaybe<Scalars['String']['input']>>>;
  username?: InputMaybe<Scalars['String']['input']>;
};

export type UpdateWikiDocumentInput = {
  color?: InputMaybe<Scalars['String']['input']>;
  emoji?: InputMaybe<Scalars['String']['input']>;
  icon?: InputMaybe<Scalars['String']['input']>;
  pageType?: InputMaybe<Scalars['String']['input']>;
  parentDocumentId?: InputMaybe<Scalars['ID']['input']>;
  sortOrder?: InputMaybe<Scalars['String']['input']>;
  status?: InputMaybe<WikiDocumentStatus>;
  tags?: InputMaybe<Array<Scalars['String']['input']>>;
  title?: InputMaybe<Scalars['String']['input']>;
};

export type User = {
  active: Scalars['Boolean']['output'];
  authSource: Scalars['String']['output'];
  completedGuides: Array<Scalars['String']['output']>;
  createdAt: Scalars['String']['output'];
  frequentIcons: Array<Scalars['String']['output']>;
  hiddenIdentities: Array<Scalars['String']['output']>;
  id: Scalars['ID']['output'];
  recentOperations: Array<Operation>;
  roles: Array<Scalars['String']['output']>;
  skillDownloadedAt?: Maybe<Scalars['String']['output']>;
  skillDownloadedVersion?: Maybe<Scalars['Int']['output']>;
  skillUpdateSnoozedVersion?: Maybe<Scalars['Int']['output']>;
  updatedAt: Scalars['String']['output'];
  username: Scalars['String']['output'];
};

export type UserConnection = {
  edges: Array<UserEdge>;
  pageInfo: PageInfo;
  totalCount: Scalars['Int']['output'];
};

export type UserEdge = {
  cursor: Scalars['String']['output'];
  node: User;
};

export type UserEvent = {
  action: EventAction;
  user?: Maybe<User>;
  userId: Scalars['ID']['output'];
  username?: Maybe<Scalars['String']['output']>;
};

export type UserSortField =
  | 'CREATED_AT'
  | 'USERNAME';

export type UserSuggestion = {
  id: Scalars['ID']['output'];
  username: Scalars['String']['output'];
};

export type WikiDocument = {
  ancestors: Array<WikiDocumentAncestor>;
  backlinks: Array<WikiDocument>;
  checklistAnswered: Scalars['Int']['output'];
  checklistRequired: Scalars['Int']['output'];
  checklistTotal: Scalars['Int']['output'];
  childCount: Scalars['Int']['output'];
  childDocuments: Array<WikiDocument>;
  color: Scalars['String']['output'];
  content: Scalars['String']['output'];
  createdAt: Scalars['String']['output'];
  createdBy: User;
  deletedAt?: Maybe<Scalars['String']['output']>;
  deletedBy?: Maybe<User>;
  emoji: Scalars['String']['output'];
  excerpt: Scalars['String']['output'];
  hasContent: Scalars['Boolean']['output'];
  icon: Scalars['String']['output'];
  id: Scalars['ID']['output'];
  isTemplate: Scalars['Boolean']['output'];
  kind: WikiDocumentKind;
  lastBackupAt?: Maybe<Scalars['String']['output']>;
  lastUpdatedAt?: Maybe<Scalars['String']['output']>;
  lastUpdatedBy?: Maybe<User>;
  operationId: Scalars['ID']['output'];
  pageType?: Maybe<Scalars['String']['output']>;
  parentDocument?: Maybe<WikiDocument>;
  parentDocumentId?: Maybe<Scalars['ID']['output']>;
  sortOrder: Scalars['String']['output'];
  sourceTemplateId?: Maybe<Scalars['ID']['output']>;
  status: WikiDocumentStatus;
  tags: Array<Scalars['String']['output']>;
  taskBacklinks: Array<Task>;
  title: Scalars['String']['output'];
  updatedAt: Scalars['String']['output'];
};


export type WikiDocumentExcerptArgs = {
  maxLength?: InputMaybe<Scalars['Int']['input']>;
};

export type WikiDocumentAncestor = {
  color: Scalars['String']['output'];
  emoji: Scalars['String']['output'];
  icon: Scalars['String']['output'];
  id: Scalars['ID']['output'];
  isDeleted: Scalars['Boolean']['output'];
  kind: WikiDocumentKind;
  title: Scalars['String']['output'];
};

export type WikiDocumentBackup = {
  content: Scalars['String']['output'];
  contentLength: Scalars['Int']['output'];
  createdAt: Scalars['String']['output'];
  createdBy?: Maybe<User>;
  description: Scalars['String']['output'];
  documentId: Scalars['ID']['output'];
  id: Scalars['ID']['output'];
  kind: WikiDocumentKind;
  title: Scalars['String']['output'];
  trigger: WikiDocumentBackupTrigger;
};

export type WikiDocumentBackupConnection = {
  edges: Array<WikiDocumentBackupEdge>;
  pageInfo: PageInfo;
  totalCount: Scalars['Int']['output'];
};

export type WikiDocumentBackupEdge = {
  cursor: Scalars['String']['output'];
  node: WikiDocumentBackup;
};

export type WikiDocumentBackupTrigger =
  | 'AUTO'
  | 'MANUAL';

export type WikiDocumentConnection = {
  edges: Array<WikiDocumentEdge>;
  pageInfo: PageInfo;
  totalCount: Scalars['Int']['output'];
};

export type WikiDocumentEdge = {
  cursor: Scalars['String']['output'];
  node: WikiDocument;
};

export type WikiDocumentEditor = {
  connectedAt: Scalars['String']['output'];
  userId: Scalars['ID']['output'];
  username: Scalars['String']['output'];
};

export type WikiDocumentEvent = {
  action: EventAction;
  document?: Maybe<WikiDocument>;
  documentId: Scalars['ID']['output'];
  operationId: Scalars['ID']['output'];
  parentDocumentId?: Maybe<Scalars['ID']['output']>;
  previousParentDocumentId?: Maybe<Scalars['ID']['output']>;
};

export type WikiDocumentKind =
  | 'DOCUMENT'
  | 'DRAWING';

export type WikiDocumentPresence = {
  activeEditors: Array<WikiDocumentEditor>;
  documentId: Scalars['ID']['output'];
};

export type WikiDocumentPresenceEvent = {
  action: PresenceAction;
  documentId: Scalars['ID']['output'];
  operationId: Scalars['ID']['output'];
  userId: Scalars['ID']['output'];
  username: Scalars['String']['output'];
};

export type WikiDocumentSort =
  | 'RECENTLY_CREATED'
  | 'RECENTLY_UPDATED';

export type WikiDocumentStatus =
  | 'DEPRECATED'
  | 'DRAFT'
  | 'STABLE';

export type WikiDocumentVisit = {
  document: WikiDocument;
  id: Scalars['ID']['output'];
  visitedAt: Scalars['String']['output'];
};

export type WikiDocumentVisitConnection = {
  edges: Array<WikiDocumentVisitEdge>;
  pageInfo: PageInfo;
  totalCount: Scalars['Int']['output'];
};

export type WikiDocumentVisitEdge = {
  cursor: Scalars['String']['output'];
  node: WikiDocumentVisit;
};

export type WikiPageTypeCount = {
  count: Scalars['Int']['output'];
  pageType: Scalars['String']['output'];
};

export type WikiSearchConnection = {
  hasMore: Scalars['Boolean']['output'];
  hits: Array<WikiSearchHit>;
  total: Scalars['Int']['output'];
};

export type WikiSearchHit = {
  document: WikiDocument;
  matchRanges: Array<WikiSearchMatchRange>;
  score?: Maybe<Scalars['Float']['output']>;
  snippet: Scalars['String']['output'];
};

export type WikiSearchMatchRange = {
  end: Scalars['Int']['output'];
  start: Scalars['Int']['output'];
};

export type WikiTagCount = {
  count: Scalars['Int']['output'];
  tag: Scalars['String']['output'];
};

export type AgentActionFieldsFragment = { id: string, agentKeyId: string, agentName: string, tool: string, write: boolean, outcome: AgentActionOutcome, error?: string | null, arguments: string, durationMs: number, occurredAt: string, operation?: { id: string, name: string } | null };

export type MyAgentActionsQueryVariables = Exact<{
  agentKeyId?: InputMaybe<Scalars['ID']['input']>;
  operationId?: InputMaybe<Scalars['ID']['input']>;
  writesOnly?: InputMaybe<Scalars['Boolean']['input']>;
  outcomes?: InputMaybe<Array<AgentActionOutcome> | AgentActionOutcome>;
  first?: InputMaybe<Scalars['Int']['input']>;
  after?: InputMaybe<Scalars['String']['input']>;
}>;


export type MyAgentActionsQuery = { myAgentActions: { totalCount: number, edges: Array<{ cursor: string, node: { id: string, agentKeyId: string, agentName: string, tool: string, write: boolean, outcome: AgentActionOutcome, error?: string | null, arguments: string, durationMs: number, occurredAt: string, operation?: { id: string, name: string } | null } }>, pageInfo: { hasNextPage: boolean, endCursor?: string | null } } };

export type MyAgentActivitySummaryQueryVariables = Exact<{ [key: string]: never; }>;


export type MyAgentActivitySummaryQuery = { myAgentActivitySummary: Array<{ agentKeyId: string, agentName: string, actions: number, operations: number, lastSeen: string }> };

export type MyAgentActionOccurredSubscriptionVariables = Exact<{ [key: string]: never; }>;


export type MyAgentActionOccurredSubscription = { myAgentActionOccurred: { agentKeyId: string, agentName: string, tool: string, write: boolean, outcome: string, operationId: string } };

export type AgentActivitySubscriptionVariables = Exact<{
  operationId: Scalars['ID']['input'];
}>;


export type AgentActivitySubscription = { agentActivity: { operationId: string, agentKeyId: string, agentName: string, agentLabel: string, ownerUserId: string, tool: string, write: boolean, outcome: string, summary: string } };

export type AgentKeyFieldsFragment = { id: string, keyId: string, name: string, enabled: boolean, maxRole: OperationRole, allowWrites: boolean, lastUsedAt?: string | null, createdAt: string, updatedAt: string, operationScopes: Array<{ id: string, name: string }> };

export type MyAgentKeysQueryVariables = Exact<{ [key: string]: never; }>;


export type MyAgentKeysQuery = { myAgentKeys: Array<{ id: string, keyId: string, name: string, enabled: boolean, maxRole: OperationRole, allowWrites: boolean, lastUsedAt?: string | null, createdAt: string, updatedAt: string, operationScopes: Array<{ id: string, name: string }> }> };

export type CreateAgentKeyMutationVariables = Exact<{
  input: CreateAgentKeyInput;
}>;


export type CreateAgentKeyMutation = { createAgentKey: { token: string, agentKey: { id: string, keyId: string, name: string, enabled: boolean, maxRole: OperationRole, allowWrites: boolean, lastUsedAt?: string | null, createdAt: string, updatedAt: string, operationScopes: Array<{ id: string, name: string }> } } };

export type RegenerateAgentKeyMutationVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type RegenerateAgentKeyMutation = { regenerateAgentKey: { token: string, agentKey: { id: string, keyId: string, name: string, enabled: boolean, maxRole: OperationRole, allowWrites: boolean, lastUsedAt?: string | null, createdAt: string, updatedAt: string, operationScopes: Array<{ id: string, name: string }> } } };

export type UpdateAgentKeyMutationVariables = Exact<{
  id: Scalars['ID']['input'];
  input: UpdateAgentKeyInput;
}>;


export type UpdateAgentKeyMutation = { updateAgentKey: { id: string, keyId: string, name: string, enabled: boolean, maxRole: OperationRole, allowWrites: boolean, lastUsedAt?: string | null, createdAt: string, updatedAt: string, operationScopes: Array<{ id: string, name: string }> } };

export type SetAgentKeyEnabledMutationVariables = Exact<{
  id: Scalars['ID']['input'];
  enabled: Scalars['Boolean']['input'];
}>;


export type SetAgentKeyEnabledMutation = { setAgentKeyEnabled: { id: string, keyId: string, name: string, enabled: boolean, maxRole: OperationRole, allowWrites: boolean, lastUsedAt?: string | null, createdAt: string, updatedAt: string, operationScopes: Array<{ id: string, name: string }> } };

export type DeleteAgentKeyMutationVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type DeleteAgentKeyMutation = { deleteAgentKey: boolean };

export type ApiKeyFieldsFragment = { id: string, keyId: string, enabled: boolean, lastUsedAt?: string | null, createdAt: string, updatedAt: string };

export type MyApiKeyQueryVariables = Exact<{ [key: string]: never; }>;


export type MyApiKeyQuery = { myAPIKey?: { id: string, keyId: string, enabled: boolean, lastUsedAt?: string | null, createdAt: string, updatedAt: string } | null };

export type CreateMyApiKeyMutationVariables = Exact<{ [key: string]: never; }>;


export type CreateMyApiKeyMutation = { createMyAPIKey: { token: string, apiKey: { id: string, keyId: string, enabled: boolean, lastUsedAt?: string | null, createdAt: string, updatedAt: string } } };

export type RegenerateMyApiKeyMutationVariables = Exact<{ [key: string]: never; }>;


export type RegenerateMyApiKeyMutation = { regenerateMyAPIKey: { token: string, apiKey: { id: string, keyId: string, enabled: boolean, lastUsedAt?: string | null, createdAt: string, updatedAt: string } } };

export type SetMyApiKeyEnabledMutationVariables = Exact<{
  enabled: Scalars['Boolean']['input'];
}>;


export type SetMyApiKeyEnabledMutation = { setMyAPIKeyEnabled: { id: string, keyId: string, enabled: boolean, lastUsedAt?: string | null, createdAt: string, updatedAt: string } };

export type DeleteMyApiKeyMutationVariables = Exact<{ [key: string]: never; }>;


export type DeleteMyApiKeyMutation = { deleteMyAPIKey: boolean };

export type CredentialCommentFieldsFragment = { id: string, text: string, createdAt: string, updatedAt: string, author?: { id: string, username: string } | null };

export type CredentialFieldsFragment = { id: string, operationId: string, name: string, type: CredentialType, username: string, password: string, validity: CredentialValidity, tags: Array<string>, viewerCanModerateComments: boolean, backlinkCount: number, createdAt: string, updatedAt: string, keys: Array<{ name: string, content: string }>, properties: Array<{ name: string, value: string }>, comments: Array<{ id: string, text: string, createdAt: string, updatedAt: string, author?: { id: string, username: string } | null }>, createdBy?: { id: string, username: string } | null };

export type CredentialFieldsWithOperationFragment = { id: string, operationId: string, name: string, type: CredentialType, username: string, password: string, validity: CredentialValidity, tags: Array<string>, viewerCanModerateComments: boolean, backlinkCount: number, createdAt: string, updatedAt: string, operation: { id: string, name: string }, keys: Array<{ name: string, content: string }>, properties: Array<{ name: string, value: string }>, comments: Array<{ id: string, text: string, createdAt: string, updatedAt: string, author?: { id: string, username: string } | null }>, createdBy?: { id: string, username: string } | null };

export type CredentialChipFieldsFragment = { id: string, operationId: string, name: string, type: CredentialType, username: string, password: string, validity: CredentialValidity, keys: Array<{ name: string, content: string }>, properties: Array<{ name: string, value: string }> };

export type CredentialChipQueryVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type CredentialChipQuery = { credential: { id: string, operationId: string, name: string, type: CredentialType, username: string, password: string, validity: CredentialValidity, keys: Array<{ name: string, content: string }>, properties: Array<{ name: string, value: string }> } };

export type CredentialQueryVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type CredentialQuery = { credential: { id: string, operationId: string, name: string, type: CredentialType, username: string, password: string, validity: CredentialValidity, tags: Array<string>, viewerCanModerateComments: boolean, backlinkCount: number, createdAt: string, updatedAt: string, keys: Array<{ name: string, content: string }>, properties: Array<{ name: string, value: string }>, comments: Array<{ id: string, text: string, createdAt: string, updatedAt: string, author?: { id: string, username: string } | null }>, createdBy?: { id: string, username: string } | null } };

export type CredentialsQueryVariables = Exact<{
  operationId: Scalars['ID']['input'];
  search?: InputMaybe<Scalars['String']['input']>;
  searchFields?: InputMaybe<Array<CredentialSearchField> | CredentialSearchField>;
  type?: InputMaybe<CredentialType>;
  tags?: InputMaybe<Array<Scalars['String']['input']> | Scalars['String']['input']>;
  validity?: InputMaybe<Array<CredentialValidity> | CredentialValidity>;
  sortBy?: InputMaybe<CredentialSortField>;
  sortDirection?: InputMaybe<SortDirection>;
  first?: InputMaybe<Scalars['Int']['input']>;
  after?: InputMaybe<Scalars['String']['input']>;
}>;


export type CredentialsQuery = { credentials: { totalCount: number, edges: Array<{ cursor: string, node: { id: string, operationId: string, name: string, type: CredentialType, username: string, password: string, validity: CredentialValidity, tags: Array<string>, viewerCanModerateComments: boolean, backlinkCount: number, createdAt: string, updatedAt: string, keys: Array<{ name: string, content: string }>, properties: Array<{ name: string, value: string }>, comments: Array<{ id: string, text: string, createdAt: string, updatedAt: string, author?: { id: string, username: string } | null }>, createdBy?: { id: string, username: string } | null } }>, pageInfo: { hasNextPage: boolean, endCursor?: string | null } } };

export type CredentialTagsQueryVariables = Exact<{
  operationId: Scalars['ID']['input'];
}>;


export type CredentialTagsQuery = { credentialTags: Array<string> };

export type CredentialSourceHashesQueryVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type CredentialSourceHashesQuery = { credential: { id: string, sourceHashes: Array<{ id: string, value: string, status: HashStatus }> } };

export type CredentialBacklinksQueryVariables = Exact<{
  credentialId: Scalars['ID']['input'];
}>;


export type CredentialBacklinksQuery = { wikiDocumentsReferencingCredential: Array<{ id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind, updatedAt: string, ancestors: Array<{ id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind, isDeleted: boolean }> }> };

export type MyCredentialsQueryVariables = Exact<{
  operationIds?: InputMaybe<Array<Scalars['ID']['input']> | Scalars['ID']['input']>;
  search?: InputMaybe<Scalars['String']['input']>;
  searchFields?: InputMaybe<Array<CredentialSearchField> | CredentialSearchField>;
  type?: InputMaybe<CredentialType>;
  tags?: InputMaybe<Array<Scalars['String']['input']> | Scalars['String']['input']>;
  validity?: InputMaybe<Array<CredentialValidity> | CredentialValidity>;
  sortBy?: InputMaybe<CredentialSortField>;
  sortDirection?: InputMaybe<SortDirection>;
  first?: InputMaybe<Scalars['Int']['input']>;
  after?: InputMaybe<Scalars['String']['input']>;
}>;


export type MyCredentialsQuery = { myCredentials: { totalCount: number, edges: Array<{ cursor: string, node: { id: string, operationId: string, name: string, type: CredentialType, username: string, password: string, validity: CredentialValidity, tags: Array<string>, viewerCanModerateComments: boolean, backlinkCount: number, createdAt: string, updatedAt: string, operation: { id: string, name: string }, keys: Array<{ name: string, content: string }>, properties: Array<{ name: string, value: string }>, comments: Array<{ id: string, text: string, createdAt: string, updatedAt: string, author?: { id: string, username: string } | null }>, createdBy?: { id: string, username: string } | null } }>, pageInfo: { hasNextPage: boolean, endCursor?: string | null } } };

export type MyCredentialTagsQueryVariables = Exact<{
  operationIds?: InputMaybe<Array<Scalars['ID']['input']> | Scalars['ID']['input']>;
}>;


export type MyCredentialTagsQuery = { myCredentialTags: Array<string> };

export type CreateCredentialMutationVariables = Exact<{
  operationId: Scalars['ID']['input'];
  input: CreateCredentialInput;
}>;


export type CreateCredentialMutation = { createCredential: { id: string, operationId: string, name: string, type: CredentialType, username: string, password: string, validity: CredentialValidity, tags: Array<string>, viewerCanModerateComments: boolean, backlinkCount: number, createdAt: string, updatedAt: string, keys: Array<{ name: string, content: string }>, properties: Array<{ name: string, value: string }>, comments: Array<{ id: string, text: string, createdAt: string, updatedAt: string, author?: { id: string, username: string } | null }>, createdBy?: { id: string, username: string } | null } };

export type UpdateCredentialMutationVariables = Exact<{
  id: Scalars['ID']['input'];
  input: UpdateCredentialInput;
}>;


export type UpdateCredentialMutation = { updateCredential: { id: string, operationId: string, name: string, type: CredentialType, username: string, password: string, validity: CredentialValidity, tags: Array<string>, viewerCanModerateComments: boolean, backlinkCount: number, createdAt: string, updatedAt: string, keys: Array<{ name: string, content: string }>, properties: Array<{ name: string, value: string }>, comments: Array<{ id: string, text: string, createdAt: string, updatedAt: string, author?: { id: string, username: string } | null }>, createdBy?: { id: string, username: string } | null } };

export type DeleteCredentialMutationVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type DeleteCredentialMutation = { deleteCredential: boolean };

export type AddCredentialCommentMutationVariables = Exact<{
  credentialId: Scalars['ID']['input'];
  text: Scalars['String']['input'];
}>;


export type AddCredentialCommentMutation = { addCredentialComment: { id: string, operationId: string, name: string, type: CredentialType, username: string, password: string, validity: CredentialValidity, tags: Array<string>, viewerCanModerateComments: boolean, backlinkCount: number, createdAt: string, updatedAt: string, keys: Array<{ name: string, content: string }>, properties: Array<{ name: string, value: string }>, comments: Array<{ id: string, text: string, createdAt: string, updatedAt: string, author?: { id: string, username: string } | null }>, createdBy?: { id: string, username: string } | null } };

export type UpdateCredentialCommentMutationVariables = Exact<{
  credentialId: Scalars['ID']['input'];
  commentId: Scalars['ID']['input'];
  text: Scalars['String']['input'];
}>;


export type UpdateCredentialCommentMutation = { updateCredentialComment: { id: string, operationId: string, name: string, type: CredentialType, username: string, password: string, validity: CredentialValidity, tags: Array<string>, viewerCanModerateComments: boolean, backlinkCount: number, createdAt: string, updatedAt: string, keys: Array<{ name: string, content: string }>, properties: Array<{ name: string, value: string }>, comments: Array<{ id: string, text: string, createdAt: string, updatedAt: string, author?: { id: string, username: string } | null }>, createdBy?: { id: string, username: string } | null } };

export type DeleteCredentialCommentMutationVariables = Exact<{
  credentialId: Scalars['ID']['input'];
  commentId: Scalars['ID']['input'];
}>;


export type DeleteCredentialCommentMutation = { deleteCredentialComment: { id: string, operationId: string, name: string, type: CredentialType, username: string, password: string, validity: CredentialValidity, tags: Array<string>, viewerCanModerateComments: boolean, backlinkCount: number, createdAt: string, updatedAt: string, keys: Array<{ name: string, content: string }>, properties: Array<{ name: string, value: string }>, comments: Array<{ id: string, text: string, createdAt: string, updatedAt: string, author?: { id: string, username: string } | null }>, createdBy?: { id: string, username: string } | null } };

export type CredentialChangedSubscriptionVariables = Exact<{
  operationId: Scalars['ID']['input'];
}>;


export type CredentialChangedSubscription = { credentialChanged: { action: EventAction, credentialId: string, operationId: string, credential?: { id: string, operationId: string, name: string, type: CredentialType, username: string, password: string, validity: CredentialValidity, tags: Array<string>, viewerCanModerateComments: boolean, backlinkCount: number, createdAt: string, updatedAt: string, keys: Array<{ name: string, content: string }>, properties: Array<{ name: string, value: string }>, comments: Array<{ id: string, text: string, createdAt: string, updatedAt: string, author?: { id: string, username: string } | null }>, createdBy?: { id: string, username: string } | null } | null } };

export type MyCredentialChangedSubscriptionVariables = Exact<{
  operationIds?: InputMaybe<Array<Scalars['ID']['input']> | Scalars['ID']['input']>;
}>;


export type MyCredentialChangedSubscription = { myCredentialChanged: { action: EventAction, credentialId: string, operationId: string, credential?: { id: string, operationId: string, name: string, type: CredentialType, username: string, password: string, validity: CredentialValidity, tags: Array<string>, viewerCanModerateComments: boolean, backlinkCount: number, createdAt: string, updatedAt: string, operation: { id: string, name: string }, keys: Array<{ name: string, content: string }>, properties: Array<{ name: string, value: string }>, comments: Array<{ id: string, text: string, createdAt: string, updatedAt: string, author?: { id: string, username: string } | null }>, createdBy?: { id: string, username: string } | null } | null } };

export type PublishOperatorFocusMutationVariables = Exact<{
  input: OperatorFocusInput;
}>;


export type PublishOperatorFocusMutation = { publishOperatorFocus: boolean };

export type MyOperatorFocusQueryVariables = Exact<{ [key: string]: never; }>;


export type MyOperatorFocusQuery = { myOperatorFocus?: { route: string, operationId?: string | null, wikiOperationId?: string | null, wikiDocumentId?: string | null, hostId?: string | null, credentialId?: string | null, hashId?: string | null, taskId?: string | null, findingsTab?: string | null, topologyLens?: string | null, topologyFocusedNodeId?: string | null, topologyFocusedEdgeId?: string | null, searchSummary?: string | null, updatedAt?: string | null } | null };

export type HashFieldsFragment = { id: string, operationId: string, value: string, status: HashStatus, comment: string, tags: Array<string>, credentialId?: string | null, createdAt: string, updatedAt: string, createdBy?: { id: string, username: string } | null };

export type HashFieldsWithCredentialFragment = { id: string, operationId: string, value: string, status: HashStatus, comment: string, tags: Array<string>, credentialId?: string | null, createdAt: string, updatedAt: string, credential?: { id: string, name: string, type: CredentialType, username: string } | null, createdBy?: { id: string, username: string } | null };

export type HashFieldsWithOperationFragment = { id: string, operationId: string, value: string, status: HashStatus, comment: string, tags: Array<string>, credentialId?: string | null, createdAt: string, updatedAt: string, operation: { id: string, name: string }, createdBy?: { id: string, username: string } | null };

export type HashChipFieldsFragment = { id: string, operationId: string, value: string, status: HashStatus, credentialId?: string | null };

export type HashChipQueryVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type HashChipQuery = { hash: { id: string, operationId: string, value: string, status: HashStatus, credentialId?: string | null } };

export type HashQueryVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type HashQuery = { hash: { id: string, operationId: string, value: string, status: HashStatus, comment: string, tags: Array<string>, credentialId?: string | null, createdAt: string, updatedAt: string, credential?: { id: string, name: string, type: CredentialType, username: string } | null, createdBy?: { id: string, username: string } | null } };

export type HashesQueryVariables = Exact<{
  operationId: Scalars['ID']['input'];
  search?: InputMaybe<Scalars['String']['input']>;
  statuses?: InputMaybe<Array<HashStatus> | HashStatus>;
  tags?: InputMaybe<Array<Scalars['String']['input']> | Scalars['String']['input']>;
  hasCredential?: InputMaybe<Scalars['Boolean']['input']>;
  first?: InputMaybe<Scalars['Int']['input']>;
  after?: InputMaybe<Scalars['String']['input']>;
}>;


export type HashesQuery = { hashes: { totalCount: number, edges: Array<{ cursor: string, node: { id: string, operationId: string, value: string, status: HashStatus, comment: string, tags: Array<string>, credentialId?: string | null, createdAt: string, updatedAt: string, createdBy?: { id: string, username: string } | null } }>, pageInfo: { hasNextPage: boolean, endCursor?: string | null } } };

export type HashTagsQueryVariables = Exact<{
  operationId: Scalars['ID']['input'];
}>;


export type HashTagsQuery = { hashTags: Array<string> };

export type HashBacklinksQueryVariables = Exact<{
  hashId: Scalars['ID']['input'];
}>;


export type HashBacklinksQuery = { wikiDocumentsReferencingHash: Array<{ id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind, updatedAt: string, ancestors: Array<{ id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind, isDeleted: boolean }> }> };

export type MyHashesQueryVariables = Exact<{
  operationIds?: InputMaybe<Array<Scalars['ID']['input']> | Scalars['ID']['input']>;
  search?: InputMaybe<Scalars['String']['input']>;
  statuses?: InputMaybe<Array<HashStatus> | HashStatus>;
  tags?: InputMaybe<Array<Scalars['String']['input']> | Scalars['String']['input']>;
  hasCredential?: InputMaybe<Scalars['Boolean']['input']>;
  first?: InputMaybe<Scalars['Int']['input']>;
  after?: InputMaybe<Scalars['String']['input']>;
}>;


export type MyHashesQuery = { myHashes: { totalCount: number, edges: Array<{ cursor: string, node: { id: string, operationId: string, value: string, status: HashStatus, comment: string, tags: Array<string>, credentialId?: string | null, createdAt: string, updatedAt: string, operation: { id: string, name: string }, createdBy?: { id: string, username: string } | null } }>, pageInfo: { hasNextPage: boolean, endCursor?: string | null } } };

export type MyHashTagsQueryVariables = Exact<{
  operationIds?: InputMaybe<Array<Scalars['ID']['input']> | Scalars['ID']['input']>;
}>;


export type MyHashTagsQuery = { myHashTags: Array<string> };

export type CreateHashMutationVariables = Exact<{
  operationId: Scalars['ID']['input'];
  input: CreateHashInput;
}>;


export type CreateHashMutation = { createHash: { id: string, operationId: string, value: string, status: HashStatus, comment: string, tags: Array<string>, credentialId?: string | null, createdAt: string, updatedAt: string, createdBy?: { id: string, username: string } | null } };

export type UpdateHashMutationVariables = Exact<{
  id: Scalars['ID']['input'];
  input: UpdateHashInput;
}>;


export type UpdateHashMutation = { updateHash: { id: string, operationId: string, value: string, status: HashStatus, comment: string, tags: Array<string>, credentialId?: string | null, createdAt: string, updatedAt: string, createdBy?: { id: string, username: string } | null } };

export type DeleteHashMutationVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type DeleteHashMutation = { deleteHash: boolean };

export type BulkImportHashesMutationVariables = Exact<{
  operationId: Scalars['ID']['input'];
  input: BulkImportHashesInput;
}>;


export type BulkImportHashesMutation = { bulkImportHashes: { added: number, skipped: number, hashes: Array<{ id: string, operationId: string, value: string, status: HashStatus, comment: string, tags: Array<string>, credentialId?: string | null, createdAt: string, updatedAt: string, createdBy?: { id: string, username: string } | null }> } };

export type MarkHashCrackedMutationVariables = Exact<{
  id: Scalars['ID']['input'];
  input: MarkHashCrackedInput;
}>;


export type MarkHashCrackedMutation = { markHashCracked: { id: string, operationId: string, value: string, status: HashStatus, comment: string, tags: Array<string>, credentialId?: string | null, createdAt: string, updatedAt: string, credential?: { id: string, name: string, type: CredentialType, username: string } | null, createdBy?: { id: string, username: string } | null } };

export type HashChangedSubscriptionVariables = Exact<{
  operationId: Scalars['ID']['input'];
}>;


export type HashChangedSubscription = { hashChanged: { action: EventAction, hashId: string, operationId: string, hash?: { id: string, operationId: string, value: string, status: HashStatus, comment: string, tags: Array<string>, credentialId?: string | null, createdAt: string, updatedAt: string, createdBy?: { id: string, username: string } | null } | null } };

export type MyHashChangedSubscriptionVariables = Exact<{
  operationIds?: InputMaybe<Array<Scalars['ID']['input']> | Scalars['ID']['input']>;
}>;


export type MyHashChangedSubscription = { myHashChanged: { action: EventAction, hashId: string, operationId: string, hash?: { id: string, operationId: string, value: string, status: HashStatus, comment: string, tags: Array<string>, credentialId?: string | null, createdAt: string, updatedAt: string, operation: { id: string, name: string }, createdBy?: { id: string, username: string } | null } | null } };

export type HostFieldsFragment = { id: string, operationId: string, hostname: string, description: string, os: string, emoji: string, icon: string, color: string, createdAt: string, updatedAt: string, interfaces: Array<{ name: string, mac: string, addresses: Array<string> }>, routes: Array<{ destination: string, gateway: string, interface: string }>, logins: Array<{ user: string, from: string, tty: string, lastSeen: string, count: number }>, createdBy?: { id: string, username: string } | null };

export type HostsQueryVariables = Exact<{
  operationId: Scalars['ID']['input'];
  search?: InputMaybe<Scalars['String']['input']>;
  sortBy?: InputMaybe<HostSortField>;
  sortDirection?: InputMaybe<SortDirection>;
  first?: InputMaybe<Scalars['Int']['input']>;
  after?: InputMaybe<Scalars['String']['input']>;
}>;


export type HostsQuery = { hosts: { totalCount: number, edges: Array<{ cursor: string, node: { id: string, operationId: string, hostname: string, description: string, os: string, emoji: string, icon: string, color: string, createdAt: string, updatedAt: string, interfaces: Array<{ name: string, mac: string, addresses: Array<string> }>, routes: Array<{ destination: string, gateway: string, interface: string }>, logins: Array<{ user: string, from: string, tty: string, lastSeen: string, count: number }>, createdBy?: { id: string, username: string } | null } }>, pageInfo: { hasNextPage: boolean, endCursor?: string | null } } };

export type HostQueryVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type HostQuery = { host: { id: string, operationId: string, hostname: string, description: string, os: string, emoji: string, icon: string, color: string, createdAt: string, updatedAt: string, interfaces: Array<{ name: string, mac: string, addresses: Array<string> }>, routes: Array<{ destination: string, gateway: string, interface: string }>, logins: Array<{ user: string, from: string, tty: string, lastSeen: string, count: number }>, createdBy?: { id: string, username: string } | null } };

export type CreateHostMutationVariables = Exact<{
  operationId: Scalars['ID']['input'];
  input: CreateHostInput;
}>;


export type CreateHostMutation = { createHost: { id: string, operationId: string, hostname: string, description: string, os: string, emoji: string, icon: string, color: string, createdAt: string, updatedAt: string, interfaces: Array<{ name: string, mac: string, addresses: Array<string> }>, routes: Array<{ destination: string, gateway: string, interface: string }>, logins: Array<{ user: string, from: string, tty: string, lastSeen: string, count: number }>, createdBy?: { id: string, username: string } | null } };

export type UpdateHostMutationVariables = Exact<{
  id: Scalars['ID']['input'];
  input: UpdateHostInput;
}>;


export type UpdateHostMutation = { updateHost: { id: string, operationId: string, hostname: string, description: string, os: string, emoji: string, icon: string, color: string, createdAt: string, updatedAt: string, interfaces: Array<{ name: string, mac: string, addresses: Array<string> }>, routes: Array<{ destination: string, gateway: string, interface: string }>, logins: Array<{ user: string, from: string, tty: string, lastSeen: string, count: number }>, createdBy?: { id: string, username: string } | null } };

export type DeleteHostMutationVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type DeleteHostMutation = { deleteHost: boolean };

export type HostChangedSubscriptionVariables = Exact<{
  operationId: Scalars['ID']['input'];
}>;


export type HostChangedSubscription = { hostChanged: { action: EventAction, hostId: string, host?: { id: string, operationId: string, hostname: string, description: string, os: string, emoji: string, icon: string, color: string, createdAt: string, updatedAt: string, interfaces: Array<{ name: string, mac: string, addresses: Array<string> }>, routes: Array<{ destination: string, gateway: string, interface: string }>, logins: Array<{ user: string, from: string, tty: string, lastSeen: string, count: number }>, createdBy?: { id: string, username: string } | null } | null } };

export type ModuleFieldsFragment = { instance: string, type: string, name: string, version: string, description: string, status: string, lastStatus: string, registeredAt: string, lastHeartbeatAt?: string | null, deregisteredAt?: string | null, deregisterReason: string, declaredDeadAt?: string | null };

export type ModulesQueryVariables = Exact<{
  status?: InputMaybe<Array<Scalars['String']['input']> | Scalars['String']['input']>;
}>;


export type ModulesQuery = { modules: Array<{ instance: string, type: string, name: string, version: string, description: string, status: string, lastStatus: string, registeredAt: string, lastHeartbeatAt?: string | null, deregisteredAt?: string | null, deregisterReason: string, declaredDeadAt?: string | null }> };

export type RemoveModuleMutationVariables = Exact<{
  instance: Scalars['ID']['input'];
}>;


export type RemoveModuleMutation = { removeModule: { instance: string, type: string, name: string, version: string, description: string, status: string, lastStatus: string, registeredAt: string, lastHeartbeatAt?: string | null, deregisteredAt?: string | null, deregisterReason: string, declaredDeadAt?: string | null } };

export type ModuleChangedSubscriptionVariables = Exact<{ [key: string]: never; }>;


export type ModuleChangedSubscription = { moduleChanged: { action: EventAction, instance: string, module?: { instance: string, type: string, name: string, version: string, description: string, status: string, lastStatus: string, registeredAt: string, lastHeartbeatAt?: string | null, deregisteredAt?: string | null, deregisterReason: string, declaredDeadAt?: string | null } | null } };

export type OperationMemberFieldsFragment = { role: OperationRole, user: { id: string, username: string, roles: Array<string>, active: boolean, createdAt: string, updatedAt: string } };

export type OperationFieldsFragment = { id: string, name: string, description: string, createdAt: string, updatedAt: string, members: Array<{ role: OperationRole, user: { id: string, username: string, roles: Array<string>, active: boolean, createdAt: string, updatedAt: string } }> };

export type OperationQueryVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type OperationQuery = { operation: { id: string, name: string, description: string, createdAt: string, updatedAt: string, members: Array<{ role: OperationRole, user: { id: string, username: string, roles: Array<string>, active: boolean, createdAt: string, updatedAt: string } }> } };

export type OperationsQueryVariables = Exact<{
  search?: InputMaybe<Scalars['String']['input']>;
  sortBy?: InputMaybe<OperationSortField>;
  sortDirection?: InputMaybe<SortDirection>;
  first?: InputMaybe<Scalars['Int']['input']>;
  after?: InputMaybe<Scalars['String']['input']>;
}>;


export type OperationsQuery = { operations: { totalCount: number, edges: Array<{ cursor: string, node: { id: string, name: string, description: string, createdAt: string, updatedAt: string, members: Array<{ role: OperationRole, user: { id: string, username: string, roles: Array<string>, active: boolean, createdAt: string, updatedAt: string } }> } }>, pageInfo: { hasNextPage: boolean, hasPreviousPage: boolean, startCursor?: string | null, endCursor?: string | null } } };

export type MyOperationRoleQueryVariables = Exact<{
  operationId: Scalars['ID']['input'];
}>;


export type MyOperationRoleQuery = { myOperationRole?: OperationRole | null };

export type CreateOperationMutationVariables = Exact<{
  input: CreateOperationInput;
}>;


export type CreateOperationMutation = { createOperation: { id: string, name: string, description: string, createdAt: string, updatedAt: string, members: Array<{ role: OperationRole, user: { id: string, username: string, roles: Array<string>, active: boolean, createdAt: string, updatedAt: string } }> } };

export type UpdateOperationMutationVariables = Exact<{
  id: Scalars['ID']['input'];
  input: UpdateOperationInput;
}>;


export type UpdateOperationMutation = { updateOperation: { id: string, name: string, description: string, createdAt: string, updatedAt: string, members: Array<{ role: OperationRole, user: { id: string, username: string, roles: Array<string>, active: boolean, createdAt: string, updatedAt: string } }> } };

export type DeleteOperationMutationVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type DeleteOperationMutation = { deleteOperation: boolean };

export type AddOperationMemberMutationVariables = Exact<{
  operationId: Scalars['ID']['input'];
  userId: Scalars['ID']['input'];
  role: OperationRole;
}>;


export type AddOperationMemberMutation = { addOperationMember: { id: string, name: string, description: string, createdAt: string, updatedAt: string, members: Array<{ role: OperationRole, user: { id: string, username: string, roles: Array<string>, active: boolean, createdAt: string, updatedAt: string } }> } };

export type RemoveOperationMemberMutationVariables = Exact<{
  operationId: Scalars['ID']['input'];
  userId: Scalars['ID']['input'];
}>;


export type RemoveOperationMemberMutation = { removeOperationMember: { id: string, name: string, description: string, createdAt: string, updatedAt: string, members: Array<{ role: OperationRole, user: { id: string, username: string, roles: Array<string>, active: boolean, createdAt: string, updatedAt: string } }> } };

export type UpdateOperationMemberRoleMutationVariables = Exact<{
  operationId: Scalars['ID']['input'];
  userId: Scalars['ID']['input'];
  role: OperationRole;
}>;


export type UpdateOperationMemberRoleMutation = { updateOperationMemberRole: { id: string, name: string, description: string, createdAt: string, updatedAt: string, members: Array<{ role: OperationRole, user: { id: string, username: string, roles: Array<string>, active: boolean, createdAt: string, updatedAt: string } }> } };

export type UserSuggestionsQueryVariables = Exact<{
  search: Scalars['String']['input'];
  first?: InputMaybe<Scalars['Int']['input']>;
}>;


export type UserSuggestionsQuery = { userSuggestions: Array<{ id: string, username: string }> };

export type OperationChangedSubscriptionVariables = Exact<{
  operationId?: InputMaybe<Scalars['ID']['input']>;
}>;


export type OperationChangedSubscription = { operationChanged: { action: EventAction, operationId: string, name?: string | null, operation?: { id: string, name: string, description: string, createdAt: string, updatedAt: string, members: Array<{ role: OperationRole, user: { id: string, username: string, roles: Array<string>, active: boolean, createdAt: string, updatedAt: string } }> } | null } };

export type OperationMemberChangedSubscriptionVariables = Exact<{
  operationId?: InputMaybe<Scalars['ID']['input']>;
}>;


export type OperationMemberChangedSubscription = { operationMemberChanged: { action: EventAction, operationId: string, userId: string } };

export type SessionFieldsFragment = { id: string, userId: string, ipAddress: string, userAgent: string, browser: string, os: string, device: string, status: SessionStatus, lastActivityAt?: string | null, isCurrent: boolean, createdAt: string, updatedAt: string, user: { id: string, username: string } };

export type MySessionsQueryVariables = Exact<{
  activeOnly?: InputMaybe<Scalars['Boolean']['input']>;
  first?: InputMaybe<Scalars['Int']['input']>;
  after?: InputMaybe<Scalars['String']['input']>;
}>;


export type MySessionsQuery = { mySessions: { totalCount: number, edges: Array<{ cursor: string, node: { id: string, userId: string, ipAddress: string, userAgent: string, browser: string, os: string, device: string, status: SessionStatus, lastActivityAt?: string | null, isCurrent: boolean, createdAt: string, updatedAt: string, user: { id: string, username: string } } }>, pageInfo: { hasNextPage: boolean, hasPreviousPage: boolean, startCursor?: string | null, endCursor?: string | null } } };

export type SessionsQueryVariables = Exact<{
  userId?: InputMaybe<Scalars['ID']['input']>;
  search?: InputMaybe<Scalars['String']['input']>;
  activeOnly?: InputMaybe<Scalars['Boolean']['input']>;
  first?: InputMaybe<Scalars['Int']['input']>;
  after?: InputMaybe<Scalars['String']['input']>;
}>;


export type SessionsQuery = { sessions: { totalCount: number, edges: Array<{ cursor: string, node: { id: string, userId: string, ipAddress: string, userAgent: string, browser: string, os: string, device: string, status: SessionStatus, lastActivityAt?: string | null, isCurrent: boolean, createdAt: string, updatedAt: string, user: { id: string, username: string } } }>, pageInfo: { hasNextPage: boolean, hasPreviousPage: boolean, startCursor?: string | null, endCursor?: string | null } } };

export type SessionQueryVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type SessionQuery = { session: { id: string, userId: string, ipAddress: string, userAgent: string, browser: string, os: string, device: string, status: SessionStatus, lastActivityAt?: string | null, isCurrent: boolean, createdAt: string, updatedAt: string, user: { id: string, username: string } } };

export type RevokeSessionMutationVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type RevokeSessionMutation = { revokeSession: boolean };

export type RevokeAllMySessionsMutationVariables = Exact<{ [key: string]: never; }>;


export type RevokeAllMySessionsMutation = { revokeAllMySessions: number };

export type AdminRevokeSessionMutationVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type AdminRevokeSessionMutation = { adminRevokeSession: boolean };

export type AdminRevokeAllUserSessionsMutationVariables = Exact<{
  userId: Scalars['ID']['input'];
}>;


export type AdminRevokeAllUserSessionsMutation = { adminRevokeAllUserSessions: number };

export type MySessionChangedSubscriptionVariables = Exact<{ [key: string]: never; }>;


export type MySessionChangedSubscription = { mySessionChanged: { action: EventAction, sessionId: string, userId: string, session?: { id: string, userId: string, ipAddress: string, userAgent: string, browser: string, os: string, device: string, status: SessionStatus, lastActivityAt?: string | null, isCurrent: boolean, createdAt: string, updatedAt: string, user: { id: string, username: string } } | null } };

export type SessionChangedSubscriptionVariables = Exact<{
  userId?: InputMaybe<Scalars['ID']['input']>;
}>;


export type SessionChangedSubscription = { sessionChanged: { action: EventAction, sessionId: string, userId: string, session?: { id: string, userId: string, ipAddress: string, userAgent: string, browser: string, os: string, device: string, status: SessionStatus, lastActivityAt?: string | null, isCurrent: boolean, createdAt: string, updatedAt: string, user: { id: string, username: string } } | null } };

export type SkillChangelogQueryVariables = Exact<{ [key: string]: never; }>;


export type SkillChangelogQuery = { skillChangelog: { currentVersion: number, releases: Array<{ version: number, date: string, notes: Array<string> }> } };

export type SnoozeSkillUpdateMutationVariables = Exact<{
  version: Scalars['Int']['input'];
}>;


export type SnoozeSkillUpdateMutation = { snoozeSkillUpdate: { id: string, skillUpdateSnoozedVersion?: number | null } };

export type SkillRegistryQueryVariables = Exact<{ [key: string]: never; }>;


export type SkillRegistryQuery = { skillRegistry: { maxUploadBytes: number, skills: Array<{ id: string, name: string, description: string, ownerUserId: string, ownerUsername: string, currentVersion: number, updatedAt: string, sizeBytes: number, mine: boolean, downloadedVersion?: number | null, downloadedAt?: string | null, snoozedVersion?: number | null, downloadUrl: string }> } };

export type SkillVersionsQueryVariables = Exact<{
  name: Scalars['String']['input'];
}>;


export type SkillVersionsQuery = { skillVersions: Array<{ version: number, uploadedAt: string, uploadedByUsername: string, sizeBytes: number, notes: string, viaAgent: boolean }> };

export type SnoozeSkillMutationVariables = Exact<{
  name: Scalars['String']['input'];
  version: Scalars['Int']['input'];
}>;


export type SnoozeSkillMutation = { snoozeSkill: { id: string, snoozedVersion?: number | null } };

export type RemoveSkillMutationVariables = Exact<{
  name: Scalars['String']['input'];
}>;


export type RemoveSkillMutation = { removeSkill: { id: string, name: string } };

export type SkillChangedSubscriptionVariables = Exact<{ [key: string]: never; }>;


export type SkillChangedSubscription = { skillChanged: { action: EventAction, skillId: string, name: string } };

export type TaskFieldsFragment = { id: string, operationId: string, name: string, description: string, riskScore: number, riskDescription: string, profitScore: number, profitDescription: string, stage: TaskStage, status: TaskStatus, summary: string, lastUpdatedAt?: string | null, deletedAt?: string | null, doneAt?: string | null, createdAt: string, updatedAt: string, assignees: Array<{ id: string, username: string }>, wikiReferences: Array<{ id: string, title: string, emoji: string }>, credentialReferences: Array<{ id: string, name: string, type: CredentialType }>, createdBy?: { id: string, username: string } | null, lastUpdatedBy?: { id: string, username: string } | null };

export type TaskBacklinkFieldsFragment = { id: string, operationId: string, name: string, stage: TaskStage, status: TaskStatus, riskScore: number, profitScore: number, assignees: Array<{ id: string, username: string }> };

export type TaskQueryVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type TaskQuery = { task: { id: string, operationId: string, name: string, description: string, riskScore: number, riskDescription: string, profitScore: number, profitDescription: string, stage: TaskStage, status: TaskStatus, summary: string, lastUpdatedAt?: string | null, deletedAt?: string | null, doneAt?: string | null, createdAt: string, updatedAt: string, assignees: Array<{ id: string, username: string }>, wikiReferences: Array<{ id: string, title: string, emoji: string }>, credentialReferences: Array<{ id: string, name: string, type: CredentialType }>, createdBy?: { id: string, username: string } | null, lastUpdatedBy?: { id: string, username: string } | null } };

export type TasksQueryVariables = Exact<{
  operationId: Scalars['ID']['input'];
  stage?: InputMaybe<TaskStage>;
  excludeStages?: InputMaybe<Array<TaskStage> | TaskStage>;
  riskScoreMin?: InputMaybe<Scalars['Int']['input']>;
  riskScoreMax?: InputMaybe<Scalars['Int']['input']>;
  profitScoreMin?: InputMaybe<Scalars['Int']['input']>;
  profitScoreMax?: InputMaybe<Scalars['Int']['input']>;
  search?: InputMaybe<Scalars['String']['input']>;
  first?: InputMaybe<Scalars['Int']['input']>;
  after?: InputMaybe<Scalars['String']['input']>;
}>;


export type TasksQuery = { tasks: { totalCount: number, edges: Array<{ cursor: string, node: { id: string, operationId: string, name: string, description: string, riskScore: number, riskDescription: string, profitScore: number, profitDescription: string, stage: TaskStage, status: TaskStatus, summary: string, lastUpdatedAt?: string | null, deletedAt?: string | null, doneAt?: string | null, createdAt: string, updatedAt: string, assignees: Array<{ id: string, username: string }>, wikiReferences: Array<{ id: string, title: string, emoji: string }>, credentialReferences: Array<{ id: string, name: string, type: CredentialType }>, createdBy?: { id: string, username: string } | null, lastUpdatedBy?: { id: string, username: string } | null } }>, pageInfo: { hasNextPage: boolean, endCursor?: string | null } } };

export type TaskTrashQueryVariables = Exact<{
  operationId: Scalars['ID']['input'];
  first?: InputMaybe<Scalars['Int']['input']>;
  after?: InputMaybe<Scalars['String']['input']>;
}>;


export type TaskTrashQuery = { taskTrash: { totalCount: number, edges: Array<{ cursor: string, node: { id: string, operationId: string, name: string, description: string, riskScore: number, riskDescription: string, profitScore: number, profitDescription: string, stage: TaskStage, status: TaskStatus, summary: string, lastUpdatedAt?: string | null, deletedAt?: string | null, doneAt?: string | null, createdAt: string, updatedAt: string, assignees: Array<{ id: string, username: string }>, wikiReferences: Array<{ id: string, title: string, emoji: string }>, credentialReferences: Array<{ id: string, name: string, type: CredentialType }>, createdBy?: { id: string, username: string } | null, lastUpdatedBy?: { id: string, username: string } | null } }>, pageInfo: { hasNextPage: boolean, endCursor?: string | null } } };

export type TasksReferencingWikiDocumentQueryVariables = Exact<{
  documentId: Scalars['ID']['input'];
}>;


export type TasksReferencingWikiDocumentQuery = { tasksReferencingWikiDocument: Array<{ id: string, operationId: string, name: string, stage: TaskStage, status: TaskStatus, riskScore: number, profitScore: number, assignees: Array<{ id: string, username: string }> }> };

export type TasksReferencingCredentialQueryVariables = Exact<{
  credentialId: Scalars['ID']['input'];
}>;


export type TasksReferencingCredentialQuery = { tasksReferencingCredential: Array<{ id: string, operationId: string, name: string, stage: TaskStage, status: TaskStatus, riskScore: number, profitScore: number, assignees: Array<{ id: string, username: string }> }> };

export type CreateTaskMutationVariables = Exact<{
  input: CreateTaskInput;
}>;


export type CreateTaskMutation = { createTask: { id: string, operationId: string, name: string, description: string, riskScore: number, riskDescription: string, profitScore: number, profitDescription: string, stage: TaskStage, status: TaskStatus, summary: string, lastUpdatedAt?: string | null, deletedAt?: string | null, doneAt?: string | null, createdAt: string, updatedAt: string, assignees: Array<{ id: string, username: string }>, wikiReferences: Array<{ id: string, title: string, emoji: string }>, credentialReferences: Array<{ id: string, name: string, type: CredentialType }>, createdBy?: { id: string, username: string } | null, lastUpdatedBy?: { id: string, username: string } | null } };

export type UpdateTaskMutationVariables = Exact<{
  id: Scalars['ID']['input'];
  input: UpdateTaskInput;
}>;


export type UpdateTaskMutation = { updateTask: { id: string, operationId: string, name: string, description: string, riskScore: number, riskDescription: string, profitScore: number, profitDescription: string, stage: TaskStage, status: TaskStatus, summary: string, lastUpdatedAt?: string | null, deletedAt?: string | null, doneAt?: string | null, createdAt: string, updatedAt: string, assignees: Array<{ id: string, username: string }>, wikiReferences: Array<{ id: string, title: string, emoji: string }>, credentialReferences: Array<{ id: string, name: string, type: CredentialType }>, createdBy?: { id: string, username: string } | null, lastUpdatedBy?: { id: string, username: string } | null } };

export type ChangeTaskStageMutationVariables = Exact<{
  input: ChangeTaskStageInput;
}>;


export type ChangeTaskStageMutation = { changeTaskStage: { id: string, operationId: string, name: string, description: string, riskScore: number, riskDescription: string, profitScore: number, profitDescription: string, stage: TaskStage, status: TaskStatus, summary: string, lastUpdatedAt?: string | null, deletedAt?: string | null, doneAt?: string | null, createdAt: string, updatedAt: string, assignees: Array<{ id: string, username: string }>, wikiReferences: Array<{ id: string, title: string, emoji: string }>, credentialReferences: Array<{ id: string, name: string, type: CredentialType }>, createdBy?: { id: string, username: string } | null, lastUpdatedBy?: { id: string, username: string } | null } };

export type SetTaskAssigneesMutationVariables = Exact<{
  taskId: Scalars['ID']['input'];
  assigneeIds: Array<Scalars['ID']['input']> | Scalars['ID']['input'];
}>;


export type SetTaskAssigneesMutation = { setTaskAssignees: { id: string, operationId: string, name: string, description: string, riskScore: number, riskDescription: string, profitScore: number, profitDescription: string, stage: TaskStage, status: TaskStatus, summary: string, lastUpdatedAt?: string | null, deletedAt?: string | null, doneAt?: string | null, createdAt: string, updatedAt: string, assignees: Array<{ id: string, username: string }>, wikiReferences: Array<{ id: string, title: string, emoji: string }>, credentialReferences: Array<{ id: string, name: string, type: CredentialType }>, createdBy?: { id: string, username: string } | null, lastUpdatedBy?: { id: string, username: string } | null } };

export type SetTaskWikiReferencesMutationVariables = Exact<{
  taskId: Scalars['ID']['input'];
  wikiIds: Array<Scalars['ID']['input']> | Scalars['ID']['input'];
}>;


export type SetTaskWikiReferencesMutation = { setTaskWikiReferences: { id: string, operationId: string, name: string, description: string, riskScore: number, riskDescription: string, profitScore: number, profitDescription: string, stage: TaskStage, status: TaskStatus, summary: string, lastUpdatedAt?: string | null, deletedAt?: string | null, doneAt?: string | null, createdAt: string, updatedAt: string, assignees: Array<{ id: string, username: string }>, wikiReferences: Array<{ id: string, title: string, emoji: string }>, credentialReferences: Array<{ id: string, name: string, type: CredentialType }>, createdBy?: { id: string, username: string } | null, lastUpdatedBy?: { id: string, username: string } | null } };

export type AddTaskWikiReferenceMutationVariables = Exact<{
  taskId: Scalars['ID']['input'];
  wikiId: Scalars['ID']['input'];
}>;


export type AddTaskWikiReferenceMutation = { addTaskWikiReference: { id: string, operationId: string, name: string, description: string, riskScore: number, riskDescription: string, profitScore: number, profitDescription: string, stage: TaskStage, status: TaskStatus, summary: string, lastUpdatedAt?: string | null, deletedAt?: string | null, doneAt?: string | null, createdAt: string, updatedAt: string, assignees: Array<{ id: string, username: string }>, wikiReferences: Array<{ id: string, title: string, emoji: string }>, credentialReferences: Array<{ id: string, name: string, type: CredentialType }>, createdBy?: { id: string, username: string } | null, lastUpdatedBy?: { id: string, username: string } | null } };

export type SetTaskCredentialReferencesMutationVariables = Exact<{
  taskId: Scalars['ID']['input'];
  credentialIds: Array<Scalars['ID']['input']> | Scalars['ID']['input'];
}>;


export type SetTaskCredentialReferencesMutation = { setTaskCredentialReferences: { id: string, operationId: string, name: string, description: string, riskScore: number, riskDescription: string, profitScore: number, profitDescription: string, stage: TaskStage, status: TaskStatus, summary: string, lastUpdatedAt?: string | null, deletedAt?: string | null, doneAt?: string | null, createdAt: string, updatedAt: string, assignees: Array<{ id: string, username: string }>, wikiReferences: Array<{ id: string, title: string, emoji: string }>, credentialReferences: Array<{ id: string, name: string, type: CredentialType }>, createdBy?: { id: string, username: string } | null, lastUpdatedBy?: { id: string, username: string } | null } };

export type DeleteTaskMutationVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type DeleteTaskMutation = { deleteTask: boolean };

export type RestoreTaskMutationVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type RestoreTaskMutation = { restoreTask: { id: string, operationId: string, name: string, description: string, riskScore: number, riskDescription: string, profitScore: number, profitDescription: string, stage: TaskStage, status: TaskStatus, summary: string, lastUpdatedAt?: string | null, deletedAt?: string | null, doneAt?: string | null, createdAt: string, updatedAt: string, assignees: Array<{ id: string, username: string }>, wikiReferences: Array<{ id: string, title: string, emoji: string }>, credentialReferences: Array<{ id: string, name: string, type: CredentialType }>, createdBy?: { id: string, username: string } | null, lastUpdatedBy?: { id: string, username: string } | null } };

export type PurgeTaskMutationVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type PurgeTaskMutation = { purgeTask: boolean };

export type TaskChangedSubscriptionVariables = Exact<{
  operationId: Scalars['ID']['input'];
}>;


export type TaskChangedSubscription = { taskChanged: { action: EventAction, taskId: string, operationId: string, task?: { id: string, operationId: string, name: string, description: string, riskScore: number, riskDescription: string, profitScore: number, profitDescription: string, stage: TaskStage, status: TaskStatus, summary: string, lastUpdatedAt?: string | null, deletedAt?: string | null, doneAt?: string | null, createdAt: string, updatedAt: string, assignees: Array<{ id: string, username: string }>, wikiReferences: Array<{ id: string, title: string, emoji: string }>, credentialReferences: Array<{ id: string, name: string, type: CredentialType }>, createdBy?: { id: string, username: string } | null, lastUpdatedBy?: { id: string, username: string } | null } | null } };

export type TimelineEventFieldsFragment = { id: string, operationId: string, topic: string, subjectKind: string, subjectId: string, subjectName: string, occurredAt: string, metadata: string, actorKind: string, actorLabel: string, actor?: { id: string, username: string } | null };

export type TimelineBucketsQueryVariables = Exact<{
  operationId: Scalars['ID']['input'];
  granularity?: InputMaybe<TimelineGranularity>;
  timezone: Scalars['String']['input'];
  from?: InputMaybe<Scalars['String']['input']>;
  to?: InputMaybe<Scalars['String']['input']>;
  types?: InputMaybe<Array<Scalars['String']['input']> | Scalars['String']['input']>;
  actorIds?: InputMaybe<Array<Scalars['ID']['input']> | Scalars['ID']['input']>;
}>;


export type TimelineBucketsQuery = { timelineBuckets: Array<{ bucketStart: string, count: number, topicCounts: Array<{ topic: string, subjectKind: string, count: number, emoji: string, icon: string, color: string }> }> };

export type TimelineEventsByDayQueryVariables = Exact<{
  operationId: Scalars['ID']['input'];
  date: Scalars['String']['input'];
  timezone: Scalars['String']['input'];
  granularity?: InputMaybe<TimelineGranularity>;
  types?: InputMaybe<Array<Scalars['String']['input']> | Scalars['String']['input']>;
  actorIds?: InputMaybe<Array<Scalars['ID']['input']> | Scalars['ID']['input']>;
  first?: InputMaybe<Scalars['Int']['input']>;
  after?: InputMaybe<Scalars['String']['input']>;
}>;


export type TimelineEventsByDayQuery = { timelineEventsByDay: { edges: Array<{ cursor: string, node: { id: string, operationId: string, topic: string, subjectKind: string, subjectId: string, subjectName: string, occurredAt: string, metadata: string, actorKind: string, actorLabel: string, actor?: { id: string, username: string } | null } }>, pageInfo: { hasNextPage: boolean, hasPreviousPage: boolean, startCursor?: string | null, endCursor?: string | null } } };

export type TimelineEventAddedSubscriptionVariables = Exact<{
  operationId: Scalars['ID']['input'];
}>;


export type TimelineEventAddedSubscription = { timelineEventAdded: { id: string, operationId: string, topic: string, subjectKind: string, subjectId: string, subjectName: string, occurredAt: string, metadata: string, actorKind: string, actorLabel: string, actor?: { id: string, username: string } | null } };

export type CreateCustomTimelineEventMutationVariables = Exact<{
  operationId: Scalars['ID']['input'];
  input: CreateCustomTimelineEventInput;
}>;


export type CreateCustomTimelineEventMutation = { createCustomTimelineEvent: { id: string, operationId: string, topic: string, subjectKind: string, subjectId: string, subjectName: string, occurredAt: string, metadata: string, actorKind: string, actorLabel: string, actor?: { id: string, username: string } | null } };

export type UpdateCustomTimelineEventMutationVariables = Exact<{
  id: Scalars['ID']['input'];
  input: UpdateCustomTimelineEventInput;
}>;


export type UpdateCustomTimelineEventMutation = { updateCustomTimelineEvent: { id: string, operationId: string, topic: string, subjectKind: string, subjectId: string, subjectName: string, occurredAt: string, metadata: string, actorKind: string, actorLabel: string, actor?: { id: string, username: string } | null } };

export type DeleteCustomTimelineEventMutationVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type DeleteCustomTimelineEventMutation = { deleteCustomTimelineEvent: boolean };

export type UserFieldsFragment = { id: string, username: string, roles: Array<string>, active: boolean, authSource: string, createdAt: string, updatedAt: string };

export type MeQueryVariables = Exact<{ [key: string]: never; }>;


export type MeQuery = { me: { hiddenIdentities: Array<string>, skillDownloadedVersion?: number | null, skillDownloadedAt?: string | null, skillUpdateSnoozedVersion?: number | null, completedGuides: Array<string>, frequentIcons: Array<string>, id: string, username: string, roles: Array<string>, active: boolean, authSource: string, createdAt: string, updatedAt: string, recentOperations: Array<{ id: string, name: string, description: string }> } };

export type CompleteGuideMutationVariables = Exact<{
  guide: Scalars['String']['input'];
}>;


export type CompleteGuideMutation = { completeGuide: { id: string, completedGuides: Array<string> } };

export type RecordIconUseMutationVariables = Exact<{
  name: Scalars['String']['input'];
}>;


export type RecordIconUseMutation = { recordIconUse: { id: string, frequentIcons: Array<string> } };

export type TouchRecentOperationMutationVariables = Exact<{
  operationId: Scalars['ID']['input'];
}>;


export type TouchRecentOperationMutation = { touchRecentOperation: { id: string, recentOperations: Array<{ id: string, name: string, description: string }> } };

export type ImportLocalPreferencesMutationVariables = Exact<{
  input: ImportLocalPreferencesInput;
}>;


export type ImportLocalPreferencesMutation = { importLocalPreferences: { id: string, frequentIcons: Array<string>, recentOperations: Array<{ id: string, name: string, description: string }> } };

export type UserQueryVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type UserQuery = { user: { id: string, username: string, roles: Array<string>, active: boolean, authSource: string, createdAt: string, updatedAt: string } };

export type UsersQueryVariables = Exact<{
  search?: InputMaybe<Scalars['String']['input']>;
  sortBy?: InputMaybe<UserSortField>;
  sortDirection?: InputMaybe<SortDirection>;
  first?: InputMaybe<Scalars['Int']['input']>;
  after?: InputMaybe<Scalars['String']['input']>;
}>;


export type UsersQuery = { users: { totalCount: number, edges: Array<{ cursor: string, node: { id: string, username: string, roles: Array<string>, active: boolean, authSource: string, createdAt: string, updatedAt: string } }>, pageInfo: { hasNextPage: boolean, hasPreviousPage: boolean, startCursor?: string | null, endCursor?: string | null } } };

export type CreateUserMutationVariables = Exact<{
  input: CreateUserInput;
}>;


export type CreateUserMutation = { createUser: { id: string, username: string, roles: Array<string>, active: boolean, authSource: string, createdAt: string, updatedAt: string } };

export type UpdateUserMutationVariables = Exact<{
  id: Scalars['ID']['input'];
  input: UpdateUserInput;
}>;


export type UpdateUserMutation = { updateUser: { id: string, username: string, roles: Array<string>, active: boolean, authSource: string, createdAt: string, updatedAt: string } };

export type DeleteUserMutationVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type DeleteUserMutation = { deleteUser: boolean };

export type UpdateOwnProfileMutationVariables = Exact<{
  input: UpdateUserInput;
}>;


export type UpdateOwnProfileMutation = { updateOwnProfile: { id: string, username: string, roles: Array<string>, active: boolean, authSource: string, createdAt: string, updatedAt: string } };

export type SetHiddenIdentitiesMutationVariables = Exact<{
  names: Array<Scalars['String']['input']> | Scalars['String']['input'];
}>;


export type SetHiddenIdentitiesMutation = { setHiddenIdentities: { id: string, hiddenIdentities: Array<string> } };

export type UserChangedSubscriptionVariables = Exact<{ [key: string]: never; }>;


export type UserChangedSubscription = { userChanged: { action: EventAction, userId: string, username?: string | null, user?: { id: string, username: string, roles: Array<string>, active: boolean, authSource: string, createdAt: string, updatedAt: string } | null } };

export type WikiDocumentTreeFieldsFragment = { id: string, operationId: string, parentDocumentId?: string | null, title: string, emoji: string, icon: string, color: string, sortOrder: string, childCount: number, hasContent: boolean, kind: WikiDocumentKind, isTemplate: boolean, sourceTemplateId?: string | null, checklistTotal: number, checklistRequired: number, checklistAnswered: number, pageType?: string | null, tags: Array<string>, status: WikiDocumentStatus, lastUpdatedAt?: string | null, updatedAt: string };

export type WikiDocumentLiteFieldsFragment = { id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind, isTemplate: boolean, deletedAt?: string | null };

export type WikiDocumentBacklinkFieldsFragment = { id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind, updatedAt: string, ancestors: Array<{ id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind, isDeleted: boolean }> };

export type WikiDocumentFieldsFragment = { id: string, operationId: string, parentDocumentId?: string | null, title: string, kind: WikiDocumentKind, content: string, emoji: string, color: string, icon: string, sortOrder: string, isTemplate: boolean, sourceTemplateId?: string | null, checklistTotal: number, checklistRequired: number, checklistAnswered: number, pageType?: string | null, tags: Array<string>, status: WikiDocumentStatus, lastUpdatedAt?: string | null, lastBackupAt?: string | null, createdAt: string, updatedAt: string, ancestors: Array<{ id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind, isDeleted: boolean }>, createdBy: { id: string, username: string }, lastUpdatedBy?: { id: string, username: string } | null };

export type WikiDocumentBackupListFieldsFragment = { id: string, documentId: string, title: string, trigger: WikiDocumentBackupTrigger, description: string, contentLength: number, createdAt: string, createdBy?: { id: string, username: string } | null };

export type WikiDocumentBackupDetailFieldsFragment = { id: string, documentId: string, title: string, kind: WikiDocumentKind, content: string, contentLength: number, trigger: WikiDocumentBackupTrigger, description: string, createdAt: string, createdBy?: { id: string, username: string } | null };

export type WikiDocumentVisitListFieldsFragment = { id: string, visitedAt: string, document: { id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind, ancestors: Array<{ id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind, isDeleted: boolean }> } };

export type WikiDocumentTreeQueryVariables = Exact<{
  operationId: Scalars['ID']['input'];
}>;


export type WikiDocumentTreeQuery = { wikiDocumentTree: Array<{ id: string, operationId: string, parentDocumentId?: string | null, title: string, emoji: string, icon: string, color: string, sortOrder: string, childCount: number, hasContent: boolean, kind: WikiDocumentKind, isTemplate: boolean, sourceTemplateId?: string | null, checklistTotal: number, checklistRequired: number, checklistAnswered: number, pageType?: string | null, tags: Array<string>, status: WikiDocumentStatus, lastUpdatedAt?: string | null, updatedAt: string }> };

export type WikiTemplatesQueryVariables = Exact<{
  operationId: Scalars['ID']['input'];
}>;


export type WikiTemplatesQuery = { wikiTemplates: Array<{ id: string, operationId: string, parentDocumentId?: string | null, title: string, emoji: string, icon: string, color: string, sortOrder: string, childCount: number, hasContent: boolean, kind: WikiDocumentKind, isTemplate: boolean, sourceTemplateId?: string | null, checklistTotal: number, checklistRequired: number, checklistAnswered: number, pageType?: string | null, tags: Array<string>, status: WikiDocumentStatus, lastUpdatedAt?: string | null, updatedAt: string }> };

export type WikiDocumentMarkdownQueryVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type WikiDocumentMarkdownQuery = { wikiDocumentMarkdown: string };

export type WikiDrawingSceneQueryVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type WikiDrawingSceneQuery = { wikiDrawingScene: string };

export type WikiDocumentChildrenQueryVariables = Exact<{
  operationId: Scalars['ID']['input'];
  parentDocumentId?: InputMaybe<Scalars['ID']['input']>;
}>;


export type WikiDocumentChildrenQuery = { wikiDocumentChildren: Array<{ id: string, operationId: string, parentDocumentId?: string | null, title: string, emoji: string, icon: string, color: string, sortOrder: string, childCount: number, hasContent: boolean, kind: WikiDocumentKind, isTemplate: boolean, sourceTemplateId?: string | null, checklistTotal: number, checklistRequired: number, checklistAnswered: number, pageType?: string | null, tags: Array<string>, status: WikiDocumentStatus, lastUpdatedAt?: string | null, updatedAt: string }> };

export type WikiDocumentTreeRevealPathQueryVariables = Exact<{
  documentId: Scalars['ID']['input'];
}>;


export type WikiDocumentTreeRevealPathQuery = { wikiDocumentTreeRevealPath: Array<{ id: string, operationId: string, parentDocumentId?: string | null, title: string, emoji: string, icon: string, color: string, sortOrder: string, childCount: number, hasContent: boolean, kind: WikiDocumentKind, isTemplate: boolean, sourceTemplateId?: string | null, checklistTotal: number, checklistRequired: number, checklistAnswered: number, pageType?: string | null, tags: Array<string>, status: WikiDocumentStatus, lastUpdatedAt?: string | null, updatedAt: string }> };

export type WikiDocumentDescendantIdsQueryVariables = Exact<{
  documentId: Scalars['ID']['input'];
}>;


export type WikiDocumentDescendantIdsQuery = { wikiDocumentDescendantIds: Array<string> };

export type WikiDocumentTrashCountQueryVariables = Exact<{
  operationId: Scalars['ID']['input'];
}>;


export type WikiDocumentTrashCountQuery = { wikiDocumentTrashCount: number };

export type WikiDocumentQueryVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type WikiDocumentQuery = { wikiDocument: { id: string, operationId: string, parentDocumentId?: string | null, title: string, kind: WikiDocumentKind, content: string, emoji: string, color: string, icon: string, sortOrder: string, isTemplate: boolean, sourceTemplateId?: string | null, checklistTotal: number, checklistRequired: number, checklistAnswered: number, pageType?: string | null, tags: Array<string>, status: WikiDocumentStatus, lastUpdatedAt?: string | null, lastBackupAt?: string | null, createdAt: string, updatedAt: string, ancestors: Array<{ id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind, isDeleted: boolean }>, createdBy: { id: string, username: string }, lastUpdatedBy?: { id: string, username: string } | null } };

export type WikiRecentDocumentsQueryVariables = Exact<{
  operationId: Scalars['ID']['input'];
  sort?: InputMaybe<WikiDocumentSort>;
  first?: InputMaybe<Scalars['Int']['input']>;
  after?: InputMaybe<Scalars['String']['input']>;
}>;


export type WikiRecentDocumentsQuery = { wikiDocuments: { totalCount: number, edges: Array<{ cursor: string, node: { id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind, parentDocumentId?: string | null, createdAt: string, updatedAt: string, lastUpdatedAt?: string | null, ancestors: Array<{ id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind, isDeleted: boolean }>, createdBy: { id: string, username: string }, lastUpdatedBy?: { id: string, username: string } | null } }>, pageInfo: { hasNextPage: boolean, endCursor?: string | null } } };

export type WikiSearchQueryVariables = Exact<{
  operationId: Scalars['ID']['input'];
  scope?: InputMaybe<Scalars['ID']['input']>;
  query: Scalars['String']['input'];
  offset?: InputMaybe<Scalars['Int']['input']>;
  limit?: InputMaybe<Scalars['Int']['input']>;
}>;


export type WikiSearchQuery = { wikiSearch: { total: number, hasMore: boolean, hits: Array<{ snippet: string, score?: number | null, document: { id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind, parentDocumentId?: string | null, ancestors: Array<{ id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind, isDeleted: boolean }>, createdBy: { id: string, username: string } }, matchRanges: Array<{ start: number, end: number }> }> } };

export type WikiDocumentLiteQueryVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type WikiDocumentLiteQuery = { wikiDocument: { id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind, isTemplate: boolean, deletedAt?: string | null } };

export type WikiDocumentPreviewQueryVariables = Exact<{
  id: Scalars['ID']['input'];
  excerptLength?: InputMaybe<Scalars['Int']['input']>;
}>;


export type WikiDocumentPreviewQuery = { wikiDocument: { excerpt: string, hasContent: boolean, kind: WikiDocumentKind, childCount: number, updatedAt: string, id: string, title: string, emoji: string, icon: string, color: string, isTemplate: boolean, deletedAt?: string | null, ancestors: Array<{ id: string, title: string, emoji: string, icon: string, color: string, isDeleted: boolean }> } };

export type WikiDocumentBacklinksQueryVariables = Exact<{
  documentId: Scalars['ID']['input'];
}>;


export type WikiDocumentBacklinksQuery = { wikiDocumentBacklinks: Array<{ id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind, updatedAt: string, ancestors: Array<{ id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind, isDeleted: boolean }> }> };

export type WikiDocumentTrashQueryVariables = Exact<{
  operationId: Scalars['ID']['input'];
  first?: InputMaybe<Scalars['Int']['input']>;
  after?: InputMaybe<Scalars['String']['input']>;
}>;


export type WikiDocumentTrashQuery = { wikiDocumentTrash: { totalCount: number, edges: Array<{ cursor: string, node: { id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind, deletedAt?: string | null, createdAt: string, deletedBy?: { id: string, username: string } | null, ancestors: Array<{ id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind, isDeleted: boolean }> } }>, pageInfo: { hasNextPage: boolean, endCursor?: string | null } } };

export type WikiDocumentBackupsQueryVariables = Exact<{
  documentId: Scalars['ID']['input'];
  first?: InputMaybe<Scalars['Int']['input']>;
  after?: InputMaybe<Scalars['String']['input']>;
}>;


export type WikiDocumentBackupsQuery = { wikiDocumentBackups: { totalCount: number, edges: Array<{ cursor: string, node: { id: string, documentId: string, title: string, trigger: WikiDocumentBackupTrigger, description: string, contentLength: number, createdAt: string, createdBy?: { id: string, username: string } | null } }>, pageInfo: { hasNextPage: boolean, endCursor?: string | null } } };

export type WikiDocumentBackupDetailQueryVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type WikiDocumentBackupDetailQuery = { wikiDocumentBackup: { id: string, documentId: string, title: string, kind: WikiDocumentKind, content: string, contentLength: number, trigger: WikiDocumentBackupTrigger, description: string, createdAt: string, createdBy?: { id: string, username: string } | null } };

export type WikiDocumentPresenceQueryVariables = Exact<{
  documentId: Scalars['ID']['input'];
}>;


export type WikiDocumentPresenceQuery = { wikiDocumentPresence: { documentId: string, activeEditors: Array<{ userId: string, username: string, connectedAt: string }> } };

export type WikiOperationPresenceQueryVariables = Exact<{
  operationId: Scalars['ID']['input'];
}>;


export type WikiOperationPresenceQuery = { wikiOperationPresence: Array<{ documentId: string, activeEditors: Array<{ userId: string, username: string, connectedAt: string }> }> };

export type WikiDocumentHistoryQueryVariables = Exact<{
  operationId: Scalars['ID']['input'];
  offset?: InputMaybe<Scalars['Int']['input']>;
  limit?: InputMaybe<Scalars['Int']['input']>;
}>;


export type WikiDocumentHistoryQuery = { wikiDocumentHistory: { totalCount: number, edges: Array<{ node: { id: string, visitedAt: string, document: { id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind, ancestors: Array<{ id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind, isDeleted: boolean }> } } }> } };

export type WikiDocumentTagsQueryVariables = Exact<{
  operationId: Scalars['ID']['input'];
}>;


export type WikiDocumentTagsQuery = { wikiDocumentTags: Array<{ tag: string, count: number }> };

export type WikiDocumentPageTypesQueryVariables = Exact<{
  operationId: Scalars['ID']['input'];
}>;


export type WikiDocumentPageTypesQuery = { wikiDocumentPageTypes: Array<{ pageType: string, count: number }> };

export type CreateWikiDocumentMutationVariables = Exact<{
  operationId: Scalars['ID']['input'];
  input: CreateWikiDocumentInput;
}>;


export type CreateWikiDocumentMutation = { createWikiDocument: { id: string, operationId: string, title: string, emoji: string, color: string, icon: string, sortOrder: string, parentDocumentId?: string | null, pageType?: string | null, tags: Array<string>, status: WikiDocumentStatus, createdAt: string, updatedAt: string, createdBy: { id: string, username: string } } };

export type UpdateWikiDocumentMutationVariables = Exact<{
  id: Scalars['ID']['input'];
  input: UpdateWikiDocumentInput;
}>;


export type UpdateWikiDocumentMutation = { updateWikiDocument: { id: string, title: string, emoji: string, color: string, icon: string, sortOrder: string, parentDocumentId?: string | null, pageType?: string | null, tags: Array<string>, status: WikiDocumentStatus, updatedAt: string } };

export type ReorderWikiDocumentSiblingsMutationVariables = Exact<{
  input: ReorderWikiDocumentSiblingsInput;
}>;


export type ReorderWikiDocumentSiblingsMutation = { reorderWikiDocumentSiblings: Array<{ id: string, sortOrder: string, parentDocumentId?: string | null, updatedAt: string }> };

export type DeleteWikiDocumentMutationVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type DeleteWikiDocumentMutation = { deleteWikiDocument: boolean };

export type DuplicateWikiDocumentMutationVariables = Exact<{
  id: Scalars['ID']['input'];
  withChildren?: InputMaybe<Scalars['Boolean']['input']>;
}>;


export type DuplicateWikiDocumentMutation = { duplicateWikiDocument: { id: string, operationId: string, title: string, emoji: string, color: string, icon: string, sortOrder: string, parentDocumentId?: string | null, createdAt: string, updatedAt: string } };

export type SetWikiDocumentTemplateMutationVariables = Exact<{
  id: Scalars['ID']['input'];
  isTemplate: Scalars['Boolean']['input'];
}>;


export type SetWikiDocumentTemplateMutation = { setWikiDocumentTemplate: { id: string, operationId: string, title: string, emoji: string, icon: string, color: string, sortOrder: string, parentDocumentId?: string | null, isTemplate: boolean, updatedAt: string } };

export type InstantiateTemplateMutationVariables = Exact<{
  templateId: Scalars['ID']['input'];
  targetOperationId: Scalars['ID']['input'];
  parentDocumentId?: InputMaybe<Scalars['ID']['input']>;
  title?: InputMaybe<Scalars['String']['input']>;
  emoji?: InputMaybe<Scalars['String']['input']>;
  icon?: InputMaybe<Scalars['String']['input']>;
  color?: InputMaybe<Scalars['String']['input']>;
}>;


export type InstantiateTemplateMutation = { instantiateTemplate: { id: string, operationId: string, title: string, emoji: string, color: string, icon: string, sortOrder: string, parentDocumentId?: string | null, isTemplate: boolean, sourceTemplateId?: string | null, checklistTotal: number, checklistRequired: number, checklistAnswered: number, createdAt: string, updatedAt: string } };

export type RestoreWikiDocumentMutationVariables = Exact<{
  id: Scalars['ID']['input'];
  cascade?: InputMaybe<Scalars['Boolean']['input']>;
}>;


export type RestoreWikiDocumentMutation = { restoreWikiDocument: { id: string, operationId: string, title: string, emoji: string, icon: string, color: string, sortOrder: string, parentDocumentId?: string | null } };

export type WikiDocumentTrashedDescendantsQueryVariables = Exact<{
  documentId: Scalars['ID']['input'];
}>;


export type WikiDocumentTrashedDescendantsQuery = { wikiDocumentTrashedDescendants: Array<{ id: string, title: string, emoji: string, icon: string, color: string, kind: WikiDocumentKind }> };

export type PermanentlyDeleteWikiDocumentMutationVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type PermanentlyDeleteWikiDocumentMutation = { permanentlyDeleteWikiDocument: boolean };

export type EmptyWikiDocumentTrashMutationVariables = Exact<{
  operationId: Scalars['ID']['input'];
}>;


export type EmptyWikiDocumentTrashMutation = { emptyWikiDocumentTrash: boolean };

export type CreateWikiDocumentBackupMutationVariables = Exact<{
  documentId: Scalars['ID']['input'];
  description?: InputMaybe<Scalars['String']['input']>;
}>;


export type CreateWikiDocumentBackupMutation = { createWikiDocumentBackup: { id: string, documentId: string, title: string, trigger: WikiDocumentBackupTrigger, description: string, createdAt: string, createdBy?: { id: string, username: string } | null } };

export type RestoreWikiDocumentBackupMutationVariables = Exact<{
  documentId: Scalars['ID']['input'];
  backupId: Scalars['ID']['input'];
}>;


export type RestoreWikiDocumentBackupMutation = { restoreWikiDocumentBackup: { id: string, title: string, content: string } };

export type DeleteWikiDocumentBackupMutationVariables = Exact<{
  id: Scalars['ID']['input'];
}>;


export type DeleteWikiDocumentBackupMutation = { deleteWikiDocumentBackup: boolean };

export type TrackWikiDocumentVisitMutationVariables = Exact<{
  documentId: Scalars['ID']['input'];
}>;


export type TrackWikiDocumentVisitMutation = { trackWikiDocumentVisit: { id: string, visitedAt: string } };

export type WikiDocumentChangedSubscriptionVariables = Exact<{
  operationId: Scalars['ID']['input'];
}>;


export type WikiDocumentChangedSubscription = { wikiDocumentChanged: { action: EventAction, documentId: string, operationId: string, parentDocumentId?: string | null, previousParentDocumentId?: string | null } };

export type WikiDocumentPresenceChangedSubscriptionVariables = Exact<{
  operationId: Scalars['ID']['input'];
}>;


export type WikiDocumentPresenceChangedSubscription = { wikiDocumentPresenceChanged: { documentId: string, operationId: string, userId: string, username: string, action: PresenceAction } };

export class TypedDocumentString<TResult, TVariables>
  extends String
  implements DocumentTypeDecoration<TResult, TVariables>
{
  __apiType?: NonNullable<DocumentTypeDecoration<TResult, TVariables>['__apiType']>;
  private value: string;
  public __meta__?: Record<string, any> | undefined;

  constructor(value: string, __meta__?: Record<string, any> | undefined) {
    super(value);
    this.value = value;
    this.__meta__ = __meta__;
  }

  override toString(): string & DocumentTypeDecoration<TResult, TVariables> {
    return this.value;
  }
}
export const AgentActionFieldsFragmentDoc = new TypedDocumentString(`
    fragment AgentActionFields on AgentAction {
  id
  agentKeyId
  agentName
  tool
  write
  outcome
  error
  arguments
  durationMs
  occurredAt
  operation {
    id
    name
  }
}
    `, {"fragmentName":"AgentActionFields"}) as unknown as TypedDocumentString<AgentActionFieldsFragment, unknown>;
export const AgentKeyFieldsFragmentDoc = new TypedDocumentString(`
    fragment AgentKeyFields on AgentKey {
  id
  keyId
  name
  enabled
  maxRole
  allowWrites
  operationScopes {
    id
    name
  }
  lastUsedAt
  createdAt
  updatedAt
}
    `, {"fragmentName":"AgentKeyFields"}) as unknown as TypedDocumentString<AgentKeyFieldsFragment, unknown>;
export const ApiKeyFieldsFragmentDoc = new TypedDocumentString(`
    fragment APIKeyFields on APIKey {
  id
  keyId
  enabled
  lastUsedAt
  createdAt
  updatedAt
}
    `, {"fragmentName":"APIKeyFields"}) as unknown as TypedDocumentString<ApiKeyFieldsFragment, unknown>;
export const CredentialCommentFieldsFragmentDoc = new TypedDocumentString(`
    fragment CredentialCommentFields on CredentialComment {
  id
  text
  createdAt
  updatedAt
  author {
    id
    username
  }
}
    `, {"fragmentName":"CredentialCommentFields"}) as unknown as TypedDocumentString<CredentialCommentFieldsFragment, unknown>;
export const CredentialFieldsFragmentDoc = new TypedDocumentString(`
    fragment CredentialFields on Credential {
  id
  operationId
  name
  type
  username
  password
  keys {
    name
    content
  }
  properties {
    name
    value
  }
  validity
  tags
  comments {
    ...CredentialCommentFields
  }
  viewerCanModerateComments
  createdBy {
    id
    username
  }
  backlinkCount
  createdAt
  updatedAt
}
    fragment CredentialCommentFields on CredentialComment {
  id
  text
  createdAt
  updatedAt
  author {
    id
    username
  }
}`, {"fragmentName":"CredentialFields"}) as unknown as TypedDocumentString<CredentialFieldsFragment, unknown>;
export const CredentialFieldsWithOperationFragmentDoc = new TypedDocumentString(`
    fragment CredentialFieldsWithOperation on Credential {
  ...CredentialFields
  operation {
    id
    name
  }
}
    fragment CredentialCommentFields on CredentialComment {
  id
  text
  createdAt
  updatedAt
  author {
    id
    username
  }
}
fragment CredentialFields on Credential {
  id
  operationId
  name
  type
  username
  password
  keys {
    name
    content
  }
  properties {
    name
    value
  }
  validity
  tags
  comments {
    ...CredentialCommentFields
  }
  viewerCanModerateComments
  createdBy {
    id
    username
  }
  backlinkCount
  createdAt
  updatedAt
}`, {"fragmentName":"CredentialFieldsWithOperation"}) as unknown as TypedDocumentString<CredentialFieldsWithOperationFragment, unknown>;
export const CredentialChipFieldsFragmentDoc = new TypedDocumentString(`
    fragment CredentialChipFields on Credential {
  id
  operationId
  name
  type
  username
  password
  validity
  keys {
    name
    content
  }
  properties {
    name
    value
  }
}
    `, {"fragmentName":"CredentialChipFields"}) as unknown as TypedDocumentString<CredentialChipFieldsFragment, unknown>;
export const HashFieldsFragmentDoc = new TypedDocumentString(`
    fragment HashFields on Hash {
  id
  operationId
  value
  status
  comment
  tags
  credentialId
  createdBy {
    id
    username
  }
  createdAt
  updatedAt
}
    `, {"fragmentName":"HashFields"}) as unknown as TypedDocumentString<HashFieldsFragment, unknown>;
export const HashFieldsWithCredentialFragmentDoc = new TypedDocumentString(`
    fragment HashFieldsWithCredential on Hash {
  ...HashFields
  credential {
    id
    name
    type
    username
  }
}
    fragment HashFields on Hash {
  id
  operationId
  value
  status
  comment
  tags
  credentialId
  createdBy {
    id
    username
  }
  createdAt
  updatedAt
}`, {"fragmentName":"HashFieldsWithCredential"}) as unknown as TypedDocumentString<HashFieldsWithCredentialFragment, unknown>;
export const HashFieldsWithOperationFragmentDoc = new TypedDocumentString(`
    fragment HashFieldsWithOperation on Hash {
  ...HashFields
  operation {
    id
    name
  }
}
    fragment HashFields on Hash {
  id
  operationId
  value
  status
  comment
  tags
  credentialId
  createdBy {
    id
    username
  }
  createdAt
  updatedAt
}`, {"fragmentName":"HashFieldsWithOperation"}) as unknown as TypedDocumentString<HashFieldsWithOperationFragment, unknown>;
export const HashChipFieldsFragmentDoc = new TypedDocumentString(`
    fragment HashChipFields on Hash {
  id
  operationId
  value
  status
  credentialId
}
    `, {"fragmentName":"HashChipFields"}) as unknown as TypedDocumentString<HashChipFieldsFragment, unknown>;
export const HostFieldsFragmentDoc = new TypedDocumentString(`
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
    `, {"fragmentName":"HostFields"}) as unknown as TypedDocumentString<HostFieldsFragment, unknown>;
export const ModuleFieldsFragmentDoc = new TypedDocumentString(`
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
    `, {"fragmentName":"ModuleFields"}) as unknown as TypedDocumentString<ModuleFieldsFragment, unknown>;
export const OperationMemberFieldsFragmentDoc = new TypedDocumentString(`
    fragment OperationMemberFields on OperationMember {
  user {
    id
    username
    roles
    active
    createdAt
    updatedAt
  }
  role
}
    `, {"fragmentName":"OperationMemberFields"}) as unknown as TypedDocumentString<OperationMemberFieldsFragment, unknown>;
export const OperationFieldsFragmentDoc = new TypedDocumentString(`
    fragment OperationFields on Operation {
  id
  name
  description
  members {
    ...OperationMemberFields
  }
  createdAt
  updatedAt
}
    fragment OperationMemberFields on OperationMember {
  user {
    id
    username
    roles
    active
    createdAt
    updatedAt
  }
  role
}`, {"fragmentName":"OperationFields"}) as unknown as TypedDocumentString<OperationFieldsFragment, unknown>;
export const SessionFieldsFragmentDoc = new TypedDocumentString(`
    fragment SessionFields on Session {
  id
  userId
  user {
    id
    username
  }
  ipAddress
  userAgent
  browser
  os
  device
  status
  lastActivityAt
  isCurrent
  createdAt
  updatedAt
}
    `, {"fragmentName":"SessionFields"}) as unknown as TypedDocumentString<SessionFieldsFragment, unknown>;
export const TaskFieldsFragmentDoc = new TypedDocumentString(`
    fragment TaskFields on Task {
  id
  operationId
  name
  description
  riskScore
  riskDescription
  profitScore
  profitDescription
  stage
  status
  summary
  assignees {
    id
    username
  }
  wikiReferences {
    id
    title
    emoji
  }
  credentialReferences {
    id
    name
    type
  }
  createdBy {
    id
    username
  }
  lastUpdatedBy {
    id
    username
  }
  lastUpdatedAt
  deletedAt
  doneAt
  createdAt
  updatedAt
}
    `, {"fragmentName":"TaskFields"}) as unknown as TypedDocumentString<TaskFieldsFragment, unknown>;
export const TaskBacklinkFieldsFragmentDoc = new TypedDocumentString(`
    fragment TaskBacklinkFields on Task {
  id
  operationId
  name
  stage
  status
  riskScore
  profitScore
  assignees {
    id
    username
  }
}
    `, {"fragmentName":"TaskBacklinkFields"}) as unknown as TypedDocumentString<TaskBacklinkFieldsFragment, unknown>;
export const TimelineEventFieldsFragmentDoc = new TypedDocumentString(`
    fragment TimelineEventFields on TimelineEvent {
  id
  operationId
  topic
  subjectKind
  subjectId
  subjectName
  occurredAt
  metadata
  actor {
    id
    username
  }
  actorKind
  actorLabel
}
    `, {"fragmentName":"TimelineEventFields"}) as unknown as TypedDocumentString<TimelineEventFieldsFragment, unknown>;
export const UserFieldsFragmentDoc = new TypedDocumentString(`
    fragment UserFields on User {
  id
  username
  roles
  active
  authSource
  createdAt
  updatedAt
}
    `, {"fragmentName":"UserFields"}) as unknown as TypedDocumentString<UserFieldsFragment, unknown>;
export const WikiDocumentTreeFieldsFragmentDoc = new TypedDocumentString(`
    fragment WikiDocumentTreeFields on WikiDocument {
  id
  operationId
  parentDocumentId
  title
  emoji
  icon
  color
  sortOrder
  childCount
  hasContent
  kind
  isTemplate
  sourceTemplateId
  checklistTotal
  checklistRequired
  checklistAnswered
  pageType
  tags
  status
  lastUpdatedAt
  updatedAt
}
    `, {"fragmentName":"WikiDocumentTreeFields"}) as unknown as TypedDocumentString<WikiDocumentTreeFieldsFragment, unknown>;
export const WikiDocumentLiteFieldsFragmentDoc = new TypedDocumentString(`
    fragment WikiDocumentLiteFields on WikiDocument {
  id
  title
  emoji
  icon
  color
  kind
  isTemplate
  deletedAt
}
    `, {"fragmentName":"WikiDocumentLiteFields"}) as unknown as TypedDocumentString<WikiDocumentLiteFieldsFragment, unknown>;
export const WikiDocumentBacklinkFieldsFragmentDoc = new TypedDocumentString(`
    fragment WikiDocumentBacklinkFields on WikiDocument {
  id
  title
  emoji
  icon
  color
  kind
  updatedAt
  ancestors {
    id
    title
    emoji
    icon
    color
    kind
    isDeleted
  }
}
    `, {"fragmentName":"WikiDocumentBacklinkFields"}) as unknown as TypedDocumentString<WikiDocumentBacklinkFieldsFragment, unknown>;
export const WikiDocumentFieldsFragmentDoc = new TypedDocumentString(`
    fragment WikiDocumentFields on WikiDocument {
  id
  operationId
  parentDocumentId
  ancestors {
    id
    title
    emoji
    icon
    color
    kind
    isDeleted
  }
  title
  kind
  content
  emoji
  color
  icon
  sortOrder
  isTemplate
  sourceTemplateId
  checklistTotal
  checklistRequired
  checklistAnswered
  pageType
  tags
  status
  createdBy {
    id
    username
  }
  lastUpdatedBy {
    id
    username
  }
  lastUpdatedAt
  lastBackupAt
  createdAt
  updatedAt
}
    `, {"fragmentName":"WikiDocumentFields"}) as unknown as TypedDocumentString<WikiDocumentFieldsFragment, unknown>;
export const WikiDocumentBackupListFieldsFragmentDoc = new TypedDocumentString(`
    fragment WikiDocumentBackupListFields on WikiDocumentBackup {
  id
  documentId
  title
  trigger
  description
  contentLength
  createdBy {
    id
    username
  }
  createdAt
}
    `, {"fragmentName":"WikiDocumentBackupListFields"}) as unknown as TypedDocumentString<WikiDocumentBackupListFieldsFragment, unknown>;
export const WikiDocumentBackupDetailFieldsFragmentDoc = new TypedDocumentString(`
    fragment WikiDocumentBackupDetailFields on WikiDocumentBackup {
  id
  documentId
  title
  kind
  content
  contentLength
  trigger
  description
  createdBy {
    id
    username
  }
  createdAt
}
    `, {"fragmentName":"WikiDocumentBackupDetailFields"}) as unknown as TypedDocumentString<WikiDocumentBackupDetailFieldsFragment, unknown>;
export const WikiDocumentVisitListFieldsFragmentDoc = new TypedDocumentString(`
    fragment WikiDocumentVisitListFields on WikiDocumentVisit {
  id
  visitedAt
  document {
    id
    title
    emoji
    icon
    color
    kind
    ancestors {
      id
      title
      emoji
      icon
      color
      kind
      isDeleted
    }
  }
}
    `, {"fragmentName":"WikiDocumentVisitListFields"}) as unknown as TypedDocumentString<WikiDocumentVisitListFieldsFragment, unknown>;
export const MyAgentActionsDocument = new TypedDocumentString(`
    query MyAgentActions($agentKeyId: ID, $operationId: ID, $writesOnly: Boolean, $outcomes: [AgentActionOutcome!], $first: Int, $after: String) {
  myAgentActions(
    agentKeyId: $agentKeyId
    operationId: $operationId
    writesOnly: $writesOnly
    outcomes: $outcomes
    first: $first
    after: $after
  ) {
    edges {
      node {
        ...AgentActionFields
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
    fragment AgentActionFields on AgentAction {
  id
  agentKeyId
  agentName
  tool
  write
  outcome
  error
  arguments
  durationMs
  occurredAt
  operation {
    id
    name
  }
}`) as unknown as TypedDocumentString<MyAgentActionsQuery, MyAgentActionsQueryVariables>;
export const MyAgentActivitySummaryDocument = new TypedDocumentString(`
    query MyAgentActivitySummary {
  myAgentActivitySummary {
    agentKeyId
    agentName
    actions
    operations
    lastSeen
  }
}
    `) as unknown as TypedDocumentString<MyAgentActivitySummaryQuery, MyAgentActivitySummaryQueryVariables>;
export const MyAgentActionOccurredDocument = new TypedDocumentString(`
    subscription MyAgentActionOccurred {
  myAgentActionOccurred {
    agentKeyId
    agentName
    tool
    write
    outcome
    operationId
  }
}
    `) as unknown as TypedDocumentString<MyAgentActionOccurredSubscription, MyAgentActionOccurredSubscriptionVariables>;
export const AgentActivityDocument = new TypedDocumentString(`
    subscription AgentActivity($operationId: ID!) {
  agentActivity(operationId: $operationId) {
    operationId
    agentKeyId
    agentName
    agentLabel
    ownerUserId
    tool
    write
    outcome
    summary
  }
}
    `) as unknown as TypedDocumentString<AgentActivitySubscription, AgentActivitySubscriptionVariables>;
export const MyAgentKeysDocument = new TypedDocumentString(`
    query MyAgentKeys {
  myAgentKeys {
    ...AgentKeyFields
  }
}
    fragment AgentKeyFields on AgentKey {
  id
  keyId
  name
  enabled
  maxRole
  allowWrites
  operationScopes {
    id
    name
  }
  lastUsedAt
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<MyAgentKeysQuery, MyAgentKeysQueryVariables>;
export const CreateAgentKeyDocument = new TypedDocumentString(`
    mutation CreateAgentKey($input: CreateAgentKeyInput!) {
  createAgentKey(input: $input) {
    agentKey {
      ...AgentKeyFields
    }
    token
  }
}
    fragment AgentKeyFields on AgentKey {
  id
  keyId
  name
  enabled
  maxRole
  allowWrites
  operationScopes {
    id
    name
  }
  lastUsedAt
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<CreateAgentKeyMutation, CreateAgentKeyMutationVariables>;
export const RegenerateAgentKeyDocument = new TypedDocumentString(`
    mutation RegenerateAgentKey($id: ID!) {
  regenerateAgentKey(id: $id) {
    agentKey {
      ...AgentKeyFields
    }
    token
  }
}
    fragment AgentKeyFields on AgentKey {
  id
  keyId
  name
  enabled
  maxRole
  allowWrites
  operationScopes {
    id
    name
  }
  lastUsedAt
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<RegenerateAgentKeyMutation, RegenerateAgentKeyMutationVariables>;
export const UpdateAgentKeyDocument = new TypedDocumentString(`
    mutation UpdateAgentKey($id: ID!, $input: UpdateAgentKeyInput!) {
  updateAgentKey(id: $id, input: $input) {
    ...AgentKeyFields
  }
}
    fragment AgentKeyFields on AgentKey {
  id
  keyId
  name
  enabled
  maxRole
  allowWrites
  operationScopes {
    id
    name
  }
  lastUsedAt
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<UpdateAgentKeyMutation, UpdateAgentKeyMutationVariables>;
export const SetAgentKeyEnabledDocument = new TypedDocumentString(`
    mutation SetAgentKeyEnabled($id: ID!, $enabled: Boolean!) {
  setAgentKeyEnabled(id: $id, enabled: $enabled) {
    ...AgentKeyFields
  }
}
    fragment AgentKeyFields on AgentKey {
  id
  keyId
  name
  enabled
  maxRole
  allowWrites
  operationScopes {
    id
    name
  }
  lastUsedAt
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<SetAgentKeyEnabledMutation, SetAgentKeyEnabledMutationVariables>;
export const DeleteAgentKeyDocument = new TypedDocumentString(`
    mutation DeleteAgentKey($id: ID!) {
  deleteAgentKey(id: $id)
}
    `) as unknown as TypedDocumentString<DeleteAgentKeyMutation, DeleteAgentKeyMutationVariables>;
export const MyApiKeyDocument = new TypedDocumentString(`
    query MyAPIKey {
  myAPIKey {
    ...APIKeyFields
  }
}
    fragment APIKeyFields on APIKey {
  id
  keyId
  enabled
  lastUsedAt
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<MyApiKeyQuery, MyApiKeyQueryVariables>;
export const CreateMyApiKeyDocument = new TypedDocumentString(`
    mutation CreateMyAPIKey {
  createMyAPIKey {
    apiKey {
      ...APIKeyFields
    }
    token
  }
}
    fragment APIKeyFields on APIKey {
  id
  keyId
  enabled
  lastUsedAt
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<CreateMyApiKeyMutation, CreateMyApiKeyMutationVariables>;
export const RegenerateMyApiKeyDocument = new TypedDocumentString(`
    mutation RegenerateMyAPIKey {
  regenerateMyAPIKey {
    apiKey {
      ...APIKeyFields
    }
    token
  }
}
    fragment APIKeyFields on APIKey {
  id
  keyId
  enabled
  lastUsedAt
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<RegenerateMyApiKeyMutation, RegenerateMyApiKeyMutationVariables>;
export const SetMyApiKeyEnabledDocument = new TypedDocumentString(`
    mutation SetMyAPIKeyEnabled($enabled: Boolean!) {
  setMyAPIKeyEnabled(enabled: $enabled) {
    ...APIKeyFields
  }
}
    fragment APIKeyFields on APIKey {
  id
  keyId
  enabled
  lastUsedAt
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<SetMyApiKeyEnabledMutation, SetMyApiKeyEnabledMutationVariables>;
export const DeleteMyApiKeyDocument = new TypedDocumentString(`
    mutation DeleteMyAPIKey {
  deleteMyAPIKey
}
    `) as unknown as TypedDocumentString<DeleteMyApiKeyMutation, DeleteMyApiKeyMutationVariables>;
export const CredentialChipDocument = new TypedDocumentString(`
    query CredentialChip($id: ID!) {
  credential(id: $id) {
    ...CredentialChipFields
  }
}
    fragment CredentialChipFields on Credential {
  id
  operationId
  name
  type
  username
  password
  validity
  keys {
    name
    content
  }
  properties {
    name
    value
  }
}`) as unknown as TypedDocumentString<CredentialChipQuery, CredentialChipQueryVariables>;
export const CredentialDocument = new TypedDocumentString(`
    query Credential($id: ID!) {
  credential(id: $id) {
    ...CredentialFields
  }
}
    fragment CredentialCommentFields on CredentialComment {
  id
  text
  createdAt
  updatedAt
  author {
    id
    username
  }
}
fragment CredentialFields on Credential {
  id
  operationId
  name
  type
  username
  password
  keys {
    name
    content
  }
  properties {
    name
    value
  }
  validity
  tags
  comments {
    ...CredentialCommentFields
  }
  viewerCanModerateComments
  createdBy {
    id
    username
  }
  backlinkCount
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<CredentialQuery, CredentialQueryVariables>;
export const CredentialsDocument = new TypedDocumentString(`
    query Credentials($operationId: ID!, $search: String, $searchFields: [CredentialSearchField!], $type: CredentialType, $tags: [String!], $validity: [CredentialValidity!], $sortBy: CredentialSortField, $sortDirection: SortDirection, $first: Int, $after: String) {
  credentials(
    operationId: $operationId
    search: $search
    searchFields: $searchFields
    type: $type
    tags: $tags
    validity: $validity
    sortBy: $sortBy
    sortDirection: $sortDirection
    first: $first
    after: $after
  ) {
    edges {
      node {
        ...CredentialFields
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
    fragment CredentialCommentFields on CredentialComment {
  id
  text
  createdAt
  updatedAt
  author {
    id
    username
  }
}
fragment CredentialFields on Credential {
  id
  operationId
  name
  type
  username
  password
  keys {
    name
    content
  }
  properties {
    name
    value
  }
  validity
  tags
  comments {
    ...CredentialCommentFields
  }
  viewerCanModerateComments
  createdBy {
    id
    username
  }
  backlinkCount
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<CredentialsQuery, CredentialsQueryVariables>;
export const CredentialTagsDocument = new TypedDocumentString(`
    query CredentialTags($operationId: ID!) {
  credentialTags(operationId: $operationId)
}
    `) as unknown as TypedDocumentString<CredentialTagsQuery, CredentialTagsQueryVariables>;
export const CredentialSourceHashesDocument = new TypedDocumentString(`
    query CredentialSourceHashes($id: ID!) {
  credential(id: $id) {
    id
    sourceHashes {
      id
      value
      status
    }
  }
}
    `) as unknown as TypedDocumentString<CredentialSourceHashesQuery, CredentialSourceHashesQueryVariables>;
export const CredentialBacklinksDocument = new TypedDocumentString(`
    query CredentialBacklinks($credentialId: ID!) {
  wikiDocumentsReferencingCredential(credentialId: $credentialId) {
    ...WikiDocumentBacklinkFields
  }
}
    fragment WikiDocumentBacklinkFields on WikiDocument {
  id
  title
  emoji
  icon
  color
  kind
  updatedAt
  ancestors {
    id
    title
    emoji
    icon
    color
    kind
    isDeleted
  }
}`) as unknown as TypedDocumentString<CredentialBacklinksQuery, CredentialBacklinksQueryVariables>;
export const MyCredentialsDocument = new TypedDocumentString(`
    query MyCredentials($operationIds: [ID!], $search: String, $searchFields: [CredentialSearchField!], $type: CredentialType, $tags: [String!], $validity: [CredentialValidity!], $sortBy: CredentialSortField, $sortDirection: SortDirection, $first: Int, $after: String) {
  myCredentials(
    operationIds: $operationIds
    search: $search
    searchFields: $searchFields
    type: $type
    tags: $tags
    validity: $validity
    sortBy: $sortBy
    sortDirection: $sortDirection
    first: $first
    after: $after
  ) {
    edges {
      node {
        ...CredentialFieldsWithOperation
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
    fragment CredentialCommentFields on CredentialComment {
  id
  text
  createdAt
  updatedAt
  author {
    id
    username
  }
}
fragment CredentialFields on Credential {
  id
  operationId
  name
  type
  username
  password
  keys {
    name
    content
  }
  properties {
    name
    value
  }
  validity
  tags
  comments {
    ...CredentialCommentFields
  }
  viewerCanModerateComments
  createdBy {
    id
    username
  }
  backlinkCount
  createdAt
  updatedAt
}
fragment CredentialFieldsWithOperation on Credential {
  ...CredentialFields
  operation {
    id
    name
  }
}`) as unknown as TypedDocumentString<MyCredentialsQuery, MyCredentialsQueryVariables>;
export const MyCredentialTagsDocument = new TypedDocumentString(`
    query MyCredentialTags($operationIds: [ID!]) {
  myCredentialTags(operationIds: $operationIds)
}
    `) as unknown as TypedDocumentString<MyCredentialTagsQuery, MyCredentialTagsQueryVariables>;
export const CreateCredentialDocument = new TypedDocumentString(`
    mutation CreateCredential($operationId: ID!, $input: CreateCredentialInput!) {
  createCredential(operationId: $operationId, input: $input) {
    ...CredentialFields
  }
}
    fragment CredentialCommentFields on CredentialComment {
  id
  text
  createdAt
  updatedAt
  author {
    id
    username
  }
}
fragment CredentialFields on Credential {
  id
  operationId
  name
  type
  username
  password
  keys {
    name
    content
  }
  properties {
    name
    value
  }
  validity
  tags
  comments {
    ...CredentialCommentFields
  }
  viewerCanModerateComments
  createdBy {
    id
    username
  }
  backlinkCount
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<CreateCredentialMutation, CreateCredentialMutationVariables>;
export const UpdateCredentialDocument = new TypedDocumentString(`
    mutation UpdateCredential($id: ID!, $input: UpdateCredentialInput!) {
  updateCredential(id: $id, input: $input) {
    ...CredentialFields
  }
}
    fragment CredentialCommentFields on CredentialComment {
  id
  text
  createdAt
  updatedAt
  author {
    id
    username
  }
}
fragment CredentialFields on Credential {
  id
  operationId
  name
  type
  username
  password
  keys {
    name
    content
  }
  properties {
    name
    value
  }
  validity
  tags
  comments {
    ...CredentialCommentFields
  }
  viewerCanModerateComments
  createdBy {
    id
    username
  }
  backlinkCount
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<UpdateCredentialMutation, UpdateCredentialMutationVariables>;
export const DeleteCredentialDocument = new TypedDocumentString(`
    mutation DeleteCredential($id: ID!) {
  deleteCredential(id: $id)
}
    `) as unknown as TypedDocumentString<DeleteCredentialMutation, DeleteCredentialMutationVariables>;
export const AddCredentialCommentDocument = new TypedDocumentString(`
    mutation AddCredentialComment($credentialId: ID!, $text: String!) {
  addCredentialComment(credentialId: $credentialId, text: $text) {
    ...CredentialFields
  }
}
    fragment CredentialCommentFields on CredentialComment {
  id
  text
  createdAt
  updatedAt
  author {
    id
    username
  }
}
fragment CredentialFields on Credential {
  id
  operationId
  name
  type
  username
  password
  keys {
    name
    content
  }
  properties {
    name
    value
  }
  validity
  tags
  comments {
    ...CredentialCommentFields
  }
  viewerCanModerateComments
  createdBy {
    id
    username
  }
  backlinkCount
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<AddCredentialCommentMutation, AddCredentialCommentMutationVariables>;
export const UpdateCredentialCommentDocument = new TypedDocumentString(`
    mutation UpdateCredentialComment($credentialId: ID!, $commentId: ID!, $text: String!) {
  updateCredentialComment(
    credentialId: $credentialId
    commentId: $commentId
    text: $text
  ) {
    ...CredentialFields
  }
}
    fragment CredentialCommentFields on CredentialComment {
  id
  text
  createdAt
  updatedAt
  author {
    id
    username
  }
}
fragment CredentialFields on Credential {
  id
  operationId
  name
  type
  username
  password
  keys {
    name
    content
  }
  properties {
    name
    value
  }
  validity
  tags
  comments {
    ...CredentialCommentFields
  }
  viewerCanModerateComments
  createdBy {
    id
    username
  }
  backlinkCount
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<UpdateCredentialCommentMutation, UpdateCredentialCommentMutationVariables>;
export const DeleteCredentialCommentDocument = new TypedDocumentString(`
    mutation DeleteCredentialComment($credentialId: ID!, $commentId: ID!) {
  deleteCredentialComment(credentialId: $credentialId, commentId: $commentId) {
    ...CredentialFields
  }
}
    fragment CredentialCommentFields on CredentialComment {
  id
  text
  createdAt
  updatedAt
  author {
    id
    username
  }
}
fragment CredentialFields on Credential {
  id
  operationId
  name
  type
  username
  password
  keys {
    name
    content
  }
  properties {
    name
    value
  }
  validity
  tags
  comments {
    ...CredentialCommentFields
  }
  viewerCanModerateComments
  createdBy {
    id
    username
  }
  backlinkCount
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<DeleteCredentialCommentMutation, DeleteCredentialCommentMutationVariables>;
export const CredentialChangedDocument = new TypedDocumentString(`
    subscription CredentialChanged($operationId: ID!) {
  credentialChanged(operationId: $operationId) {
    action
    credentialId
    operationId
    credential {
      ...CredentialFields
    }
  }
}
    fragment CredentialCommentFields on CredentialComment {
  id
  text
  createdAt
  updatedAt
  author {
    id
    username
  }
}
fragment CredentialFields on Credential {
  id
  operationId
  name
  type
  username
  password
  keys {
    name
    content
  }
  properties {
    name
    value
  }
  validity
  tags
  comments {
    ...CredentialCommentFields
  }
  viewerCanModerateComments
  createdBy {
    id
    username
  }
  backlinkCount
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<CredentialChangedSubscription, CredentialChangedSubscriptionVariables>;
export const MyCredentialChangedDocument = new TypedDocumentString(`
    subscription MyCredentialChanged($operationIds: [ID!]) {
  myCredentialChanged(operationIds: $operationIds) {
    action
    credentialId
    operationId
    credential {
      ...CredentialFieldsWithOperation
    }
  }
}
    fragment CredentialCommentFields on CredentialComment {
  id
  text
  createdAt
  updatedAt
  author {
    id
    username
  }
}
fragment CredentialFields on Credential {
  id
  operationId
  name
  type
  username
  password
  keys {
    name
    content
  }
  properties {
    name
    value
  }
  validity
  tags
  comments {
    ...CredentialCommentFields
  }
  viewerCanModerateComments
  createdBy {
    id
    username
  }
  backlinkCount
  createdAt
  updatedAt
}
fragment CredentialFieldsWithOperation on Credential {
  ...CredentialFields
  operation {
    id
    name
  }
}`) as unknown as TypedDocumentString<MyCredentialChangedSubscription, MyCredentialChangedSubscriptionVariables>;
export const PublishOperatorFocusDocument = new TypedDocumentString(`
    mutation PublishOperatorFocus($input: OperatorFocusInput!) {
  publishOperatorFocus(input: $input)
}
    `) as unknown as TypedDocumentString<PublishOperatorFocusMutation, PublishOperatorFocusMutationVariables>;
export const MyOperatorFocusDocument = new TypedDocumentString(`
    query MyOperatorFocus {
  myOperatorFocus {
    route
    operationId
    wikiOperationId
    wikiDocumentId
    hostId
    credentialId
    hashId
    taskId
    findingsTab
    topologyLens
    topologyFocusedNodeId
    topologyFocusedEdgeId
    searchSummary
    updatedAt
  }
}
    `) as unknown as TypedDocumentString<MyOperatorFocusQuery, MyOperatorFocusQueryVariables>;
export const HashChipDocument = new TypedDocumentString(`
    query HashChip($id: ID!) {
  hash(id: $id) {
    ...HashChipFields
  }
}
    fragment HashChipFields on Hash {
  id
  operationId
  value
  status
  credentialId
}`) as unknown as TypedDocumentString<HashChipQuery, HashChipQueryVariables>;
export const HashDocument = new TypedDocumentString(`
    query Hash($id: ID!) {
  hash(id: $id) {
    ...HashFieldsWithCredential
  }
}
    fragment HashFields on Hash {
  id
  operationId
  value
  status
  comment
  tags
  credentialId
  createdBy {
    id
    username
  }
  createdAt
  updatedAt
}
fragment HashFieldsWithCredential on Hash {
  ...HashFields
  credential {
    id
    name
    type
    username
  }
}`) as unknown as TypedDocumentString<HashQuery, HashQueryVariables>;
export const HashesDocument = new TypedDocumentString(`
    query Hashes($operationId: ID!, $search: String, $statuses: [HashStatus!], $tags: [String!], $hasCredential: Boolean, $first: Int, $after: String) {
  hashes(
    operationId: $operationId
    search: $search
    statuses: $statuses
    tags: $tags
    hasCredential: $hasCredential
    first: $first
    after: $after
  ) {
    edges {
      node {
        ...HashFields
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
    fragment HashFields on Hash {
  id
  operationId
  value
  status
  comment
  tags
  credentialId
  createdBy {
    id
    username
  }
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<HashesQuery, HashesQueryVariables>;
export const HashTagsDocument = new TypedDocumentString(`
    query HashTags($operationId: ID!) {
  hashTags(operationId: $operationId)
}
    `) as unknown as TypedDocumentString<HashTagsQuery, HashTagsQueryVariables>;
export const HashBacklinksDocument = new TypedDocumentString(`
    query HashBacklinks($hashId: ID!) {
  wikiDocumentsReferencingHash(hashId: $hashId) {
    ...WikiDocumentBacklinkFields
  }
}
    fragment WikiDocumentBacklinkFields on WikiDocument {
  id
  title
  emoji
  icon
  color
  kind
  updatedAt
  ancestors {
    id
    title
    emoji
    icon
    color
    kind
    isDeleted
  }
}`) as unknown as TypedDocumentString<HashBacklinksQuery, HashBacklinksQueryVariables>;
export const MyHashesDocument = new TypedDocumentString(`
    query MyHashes($operationIds: [ID!], $search: String, $statuses: [HashStatus!], $tags: [String!], $hasCredential: Boolean, $first: Int, $after: String) {
  myHashes(
    operationIds: $operationIds
    search: $search
    statuses: $statuses
    tags: $tags
    hasCredential: $hasCredential
    first: $first
    after: $after
  ) {
    edges {
      node {
        ...HashFieldsWithOperation
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
    fragment HashFields on Hash {
  id
  operationId
  value
  status
  comment
  tags
  credentialId
  createdBy {
    id
    username
  }
  createdAt
  updatedAt
}
fragment HashFieldsWithOperation on Hash {
  ...HashFields
  operation {
    id
    name
  }
}`) as unknown as TypedDocumentString<MyHashesQuery, MyHashesQueryVariables>;
export const MyHashTagsDocument = new TypedDocumentString(`
    query MyHashTags($operationIds: [ID!]) {
  myHashTags(operationIds: $operationIds)
}
    `) as unknown as TypedDocumentString<MyHashTagsQuery, MyHashTagsQueryVariables>;
export const CreateHashDocument = new TypedDocumentString(`
    mutation CreateHash($operationId: ID!, $input: CreateHashInput!) {
  createHash(operationId: $operationId, input: $input) {
    ...HashFields
  }
}
    fragment HashFields on Hash {
  id
  operationId
  value
  status
  comment
  tags
  credentialId
  createdBy {
    id
    username
  }
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<CreateHashMutation, CreateHashMutationVariables>;
export const UpdateHashDocument = new TypedDocumentString(`
    mutation UpdateHash($id: ID!, $input: UpdateHashInput!) {
  updateHash(id: $id, input: $input) {
    ...HashFields
  }
}
    fragment HashFields on Hash {
  id
  operationId
  value
  status
  comment
  tags
  credentialId
  createdBy {
    id
    username
  }
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<UpdateHashMutation, UpdateHashMutationVariables>;
export const DeleteHashDocument = new TypedDocumentString(`
    mutation DeleteHash($id: ID!) {
  deleteHash(id: $id)
}
    `) as unknown as TypedDocumentString<DeleteHashMutation, DeleteHashMutationVariables>;
export const BulkImportHashesDocument = new TypedDocumentString(`
    mutation BulkImportHashes($operationId: ID!, $input: BulkImportHashesInput!) {
  bulkImportHashes(operationId: $operationId, input: $input) {
    added
    skipped
    hashes {
      ...HashFields
    }
  }
}
    fragment HashFields on Hash {
  id
  operationId
  value
  status
  comment
  tags
  credentialId
  createdBy {
    id
    username
  }
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<BulkImportHashesMutation, BulkImportHashesMutationVariables>;
export const MarkHashCrackedDocument = new TypedDocumentString(`
    mutation MarkHashCracked($id: ID!, $input: MarkHashCrackedInput!) {
  markHashCracked(id: $id, input: $input) {
    ...HashFieldsWithCredential
  }
}
    fragment HashFields on Hash {
  id
  operationId
  value
  status
  comment
  tags
  credentialId
  createdBy {
    id
    username
  }
  createdAt
  updatedAt
}
fragment HashFieldsWithCredential on Hash {
  ...HashFields
  credential {
    id
    name
    type
    username
  }
}`) as unknown as TypedDocumentString<MarkHashCrackedMutation, MarkHashCrackedMutationVariables>;
export const HashChangedDocument = new TypedDocumentString(`
    subscription HashChanged($operationId: ID!) {
  hashChanged(operationId: $operationId) {
    action
    hashId
    operationId
    hash {
      ...HashFields
    }
  }
}
    fragment HashFields on Hash {
  id
  operationId
  value
  status
  comment
  tags
  credentialId
  createdBy {
    id
    username
  }
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<HashChangedSubscription, HashChangedSubscriptionVariables>;
export const MyHashChangedDocument = new TypedDocumentString(`
    subscription MyHashChanged($operationIds: [ID!]) {
  myHashChanged(operationIds: $operationIds) {
    action
    hashId
    operationId
    hash {
      ...HashFieldsWithOperation
    }
  }
}
    fragment HashFields on Hash {
  id
  operationId
  value
  status
  comment
  tags
  credentialId
  createdBy {
    id
    username
  }
  createdAt
  updatedAt
}
fragment HashFieldsWithOperation on Hash {
  ...HashFields
  operation {
    id
    name
  }
}`) as unknown as TypedDocumentString<MyHashChangedSubscription, MyHashChangedSubscriptionVariables>;
export const HostsDocument = new TypedDocumentString(`
    query Hosts($operationId: ID!, $search: String, $sortBy: HostSortField, $sortDirection: SortDirection, $first: Int, $after: String) {
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
}`) as unknown as TypedDocumentString<HostsQuery, HostsQueryVariables>;
export const HostDocument = new TypedDocumentString(`
    query Host($id: ID!) {
  host(id: $id) {
    ...HostFields
  }
}
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
}`) as unknown as TypedDocumentString<HostQuery, HostQueryVariables>;
export const CreateHostDocument = new TypedDocumentString(`
    mutation CreateHost($operationId: ID!, $input: CreateHostInput!) {
  createHost(operationId: $operationId, input: $input) {
    ...HostFields
  }
}
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
}`) as unknown as TypedDocumentString<CreateHostMutation, CreateHostMutationVariables>;
export const UpdateHostDocument = new TypedDocumentString(`
    mutation UpdateHost($id: ID!, $input: UpdateHostInput!) {
  updateHost(id: $id, input: $input) {
    ...HostFields
  }
}
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
}`) as unknown as TypedDocumentString<UpdateHostMutation, UpdateHostMutationVariables>;
export const DeleteHostDocument = new TypedDocumentString(`
    mutation DeleteHost($id: ID!) {
  deleteHost(id: $id)
}
    `) as unknown as TypedDocumentString<DeleteHostMutation, DeleteHostMutationVariables>;
export const HostChangedDocument = new TypedDocumentString(`
    subscription HostChanged($operationId: ID!) {
  hostChanged(operationId: $operationId) {
    action
    hostId
    host {
      ...HostFields
    }
  }
}
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
}`) as unknown as TypedDocumentString<HostChangedSubscription, HostChangedSubscriptionVariables>;
export const ModulesDocument = new TypedDocumentString(`
    query Modules($status: [String!]) {
  modules(status: $status) {
    ...ModuleFields
  }
}
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
}`) as unknown as TypedDocumentString<ModulesQuery, ModulesQueryVariables>;
export const RemoveModuleDocument = new TypedDocumentString(`
    mutation RemoveModule($instance: ID!) {
  removeModule(instance: $instance) {
    ...ModuleFields
  }
}
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
}`) as unknown as TypedDocumentString<RemoveModuleMutation, RemoveModuleMutationVariables>;
export const ModuleChangedDocument = new TypedDocumentString(`
    subscription ModuleChanged {
  moduleChanged {
    action
    instance
    module {
      ...ModuleFields
    }
  }
}
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
}`) as unknown as TypedDocumentString<ModuleChangedSubscription, ModuleChangedSubscriptionVariables>;
export const OperationDocument = new TypedDocumentString(`
    query Operation($id: ID!) {
  operation(id: $id) {
    ...OperationFields
  }
}
    fragment OperationMemberFields on OperationMember {
  user {
    id
    username
    roles
    active
    createdAt
    updatedAt
  }
  role
}
fragment OperationFields on Operation {
  id
  name
  description
  members {
    ...OperationMemberFields
  }
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<OperationQuery, OperationQueryVariables>;
export const OperationsDocument = new TypedDocumentString(`
    query Operations($search: String, $sortBy: OperationSortField, $sortDirection: SortDirection, $first: Int, $after: String) {
  operations(
    search: $search
    sortBy: $sortBy
    sortDirection: $sortDirection
    first: $first
    after: $after
  ) {
    edges {
      node {
        ...OperationFields
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
    fragment OperationMemberFields on OperationMember {
  user {
    id
    username
    roles
    active
    createdAt
    updatedAt
  }
  role
}
fragment OperationFields on Operation {
  id
  name
  description
  members {
    ...OperationMemberFields
  }
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<OperationsQuery, OperationsQueryVariables>;
export const MyOperationRoleDocument = new TypedDocumentString(`
    query MyOperationRole($operationId: ID!) {
  myOperationRole(operationId: $operationId)
}
    `) as unknown as TypedDocumentString<MyOperationRoleQuery, MyOperationRoleQueryVariables>;
export const CreateOperationDocument = new TypedDocumentString(`
    mutation CreateOperation($input: CreateOperationInput!) {
  createOperation(input: $input) {
    ...OperationFields
  }
}
    fragment OperationMemberFields on OperationMember {
  user {
    id
    username
    roles
    active
    createdAt
    updatedAt
  }
  role
}
fragment OperationFields on Operation {
  id
  name
  description
  members {
    ...OperationMemberFields
  }
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<CreateOperationMutation, CreateOperationMutationVariables>;
export const UpdateOperationDocument = new TypedDocumentString(`
    mutation UpdateOperation($id: ID!, $input: UpdateOperationInput!) {
  updateOperation(id: $id, input: $input) {
    ...OperationFields
  }
}
    fragment OperationMemberFields on OperationMember {
  user {
    id
    username
    roles
    active
    createdAt
    updatedAt
  }
  role
}
fragment OperationFields on Operation {
  id
  name
  description
  members {
    ...OperationMemberFields
  }
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<UpdateOperationMutation, UpdateOperationMutationVariables>;
export const DeleteOperationDocument = new TypedDocumentString(`
    mutation DeleteOperation($id: ID!) {
  deleteOperation(id: $id)
}
    `) as unknown as TypedDocumentString<DeleteOperationMutation, DeleteOperationMutationVariables>;
export const AddOperationMemberDocument = new TypedDocumentString(`
    mutation AddOperationMember($operationId: ID!, $userId: ID!, $role: OperationRole!) {
  addOperationMember(operationId: $operationId, userId: $userId, role: $role) {
    ...OperationFields
  }
}
    fragment OperationMemberFields on OperationMember {
  user {
    id
    username
    roles
    active
    createdAt
    updatedAt
  }
  role
}
fragment OperationFields on Operation {
  id
  name
  description
  members {
    ...OperationMemberFields
  }
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<AddOperationMemberMutation, AddOperationMemberMutationVariables>;
export const RemoveOperationMemberDocument = new TypedDocumentString(`
    mutation RemoveOperationMember($operationId: ID!, $userId: ID!) {
  removeOperationMember(operationId: $operationId, userId: $userId) {
    ...OperationFields
  }
}
    fragment OperationMemberFields on OperationMember {
  user {
    id
    username
    roles
    active
    createdAt
    updatedAt
  }
  role
}
fragment OperationFields on Operation {
  id
  name
  description
  members {
    ...OperationMemberFields
  }
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<RemoveOperationMemberMutation, RemoveOperationMemberMutationVariables>;
export const UpdateOperationMemberRoleDocument = new TypedDocumentString(`
    mutation UpdateOperationMemberRole($operationId: ID!, $userId: ID!, $role: OperationRole!) {
  updateOperationMemberRole(
    operationId: $operationId
    userId: $userId
    role: $role
  ) {
    ...OperationFields
  }
}
    fragment OperationMemberFields on OperationMember {
  user {
    id
    username
    roles
    active
    createdAt
    updatedAt
  }
  role
}
fragment OperationFields on Operation {
  id
  name
  description
  members {
    ...OperationMemberFields
  }
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<UpdateOperationMemberRoleMutation, UpdateOperationMemberRoleMutationVariables>;
export const UserSuggestionsDocument = new TypedDocumentString(`
    query UserSuggestions($search: String!, $first: Int) {
  userSuggestions(search: $search, first: $first) {
    id
    username
  }
}
    `) as unknown as TypedDocumentString<UserSuggestionsQuery, UserSuggestionsQueryVariables>;
export const OperationChangedDocument = new TypedDocumentString(`
    subscription OperationChanged($operationId: ID) {
  operationChanged(operationId: $operationId) {
    action
    operationId
    name
    operation {
      ...OperationFields
    }
  }
}
    fragment OperationMemberFields on OperationMember {
  user {
    id
    username
    roles
    active
    createdAt
    updatedAt
  }
  role
}
fragment OperationFields on Operation {
  id
  name
  description
  members {
    ...OperationMemberFields
  }
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<OperationChangedSubscription, OperationChangedSubscriptionVariables>;
export const OperationMemberChangedDocument = new TypedDocumentString(`
    subscription OperationMemberChanged($operationId: ID) {
  operationMemberChanged(operationId: $operationId) {
    action
    operationId
    userId
  }
}
    `) as unknown as TypedDocumentString<OperationMemberChangedSubscription, OperationMemberChangedSubscriptionVariables>;
export const MySessionsDocument = new TypedDocumentString(`
    query MySessions($activeOnly: Boolean, $first: Int, $after: String) {
  mySessions(activeOnly: $activeOnly, first: $first, after: $after) {
    edges {
      node {
        ...SessionFields
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
    fragment SessionFields on Session {
  id
  userId
  user {
    id
    username
  }
  ipAddress
  userAgent
  browser
  os
  device
  status
  lastActivityAt
  isCurrent
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<MySessionsQuery, MySessionsQueryVariables>;
export const SessionsDocument = new TypedDocumentString(`
    query Sessions($userId: ID, $search: String, $activeOnly: Boolean, $first: Int, $after: String) {
  sessions(
    userId: $userId
    search: $search
    activeOnly: $activeOnly
    first: $first
    after: $after
  ) {
    edges {
      node {
        ...SessionFields
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
    fragment SessionFields on Session {
  id
  userId
  user {
    id
    username
  }
  ipAddress
  userAgent
  browser
  os
  device
  status
  lastActivityAt
  isCurrent
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<SessionsQuery, SessionsQueryVariables>;
export const SessionDocument = new TypedDocumentString(`
    query Session($id: ID!) {
  session(id: $id) {
    ...SessionFields
  }
}
    fragment SessionFields on Session {
  id
  userId
  user {
    id
    username
  }
  ipAddress
  userAgent
  browser
  os
  device
  status
  lastActivityAt
  isCurrent
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<SessionQuery, SessionQueryVariables>;
export const RevokeSessionDocument = new TypedDocumentString(`
    mutation RevokeSession($id: ID!) {
  revokeSession(id: $id)
}
    `) as unknown as TypedDocumentString<RevokeSessionMutation, RevokeSessionMutationVariables>;
export const RevokeAllMySessionsDocument = new TypedDocumentString(`
    mutation RevokeAllMySessions {
  revokeAllMySessions
}
    `) as unknown as TypedDocumentString<RevokeAllMySessionsMutation, RevokeAllMySessionsMutationVariables>;
export const AdminRevokeSessionDocument = new TypedDocumentString(`
    mutation AdminRevokeSession($id: ID!) {
  adminRevokeSession(id: $id)
}
    `) as unknown as TypedDocumentString<AdminRevokeSessionMutation, AdminRevokeSessionMutationVariables>;
export const AdminRevokeAllUserSessionsDocument = new TypedDocumentString(`
    mutation AdminRevokeAllUserSessions($userId: ID!) {
  adminRevokeAllUserSessions(userId: $userId)
}
    `) as unknown as TypedDocumentString<AdminRevokeAllUserSessionsMutation, AdminRevokeAllUserSessionsMutationVariables>;
export const MySessionChangedDocument = new TypedDocumentString(`
    subscription MySessionChanged {
  mySessionChanged {
    action
    sessionId
    userId
    session {
      ...SessionFields
    }
  }
}
    fragment SessionFields on Session {
  id
  userId
  user {
    id
    username
  }
  ipAddress
  userAgent
  browser
  os
  device
  status
  lastActivityAt
  isCurrent
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<MySessionChangedSubscription, MySessionChangedSubscriptionVariables>;
export const SessionChangedDocument = new TypedDocumentString(`
    subscription SessionChanged($userId: ID) {
  sessionChanged(userId: $userId) {
    action
    sessionId
    userId
    session {
      ...SessionFields
    }
  }
}
    fragment SessionFields on Session {
  id
  userId
  user {
    id
    username
  }
  ipAddress
  userAgent
  browser
  os
  device
  status
  lastActivityAt
  isCurrent
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<SessionChangedSubscription, SessionChangedSubscriptionVariables>;
export const SkillChangelogDocument = new TypedDocumentString(`
    query SkillChangelog {
  skillChangelog {
    currentVersion
    releases {
      version
      date
      notes
    }
  }
}
    `) as unknown as TypedDocumentString<SkillChangelogQuery, SkillChangelogQueryVariables>;
export const SnoozeSkillUpdateDocument = new TypedDocumentString(`
    mutation SnoozeSkillUpdate($version: Int!) {
  snoozeSkillUpdate(version: $version) {
    id
    skillUpdateSnoozedVersion
  }
}
    `) as unknown as TypedDocumentString<SnoozeSkillUpdateMutation, SnoozeSkillUpdateMutationVariables>;
export const SkillRegistryDocument = new TypedDocumentString(`
    query SkillRegistry {
  skillRegistry {
    maxUploadBytes
    skills {
      id
      name
      description
      ownerUserId
      ownerUsername
      currentVersion
      updatedAt
      sizeBytes
      mine
      downloadedVersion
      downloadedAt
      snoozedVersion
      downloadUrl
    }
  }
}
    `) as unknown as TypedDocumentString<SkillRegistryQuery, SkillRegistryQueryVariables>;
export const SkillVersionsDocument = new TypedDocumentString(`
    query SkillVersions($name: String!) {
  skillVersions(name: $name) {
    version
    uploadedAt
    uploadedByUsername
    sizeBytes
    notes
    viaAgent
  }
}
    `) as unknown as TypedDocumentString<SkillVersionsQuery, SkillVersionsQueryVariables>;
export const SnoozeSkillDocument = new TypedDocumentString(`
    mutation SnoozeSkill($name: String!, $version: Int!) {
  snoozeSkill(name: $name, version: $version) {
    id
    snoozedVersion
  }
}
    `) as unknown as TypedDocumentString<SnoozeSkillMutation, SnoozeSkillMutationVariables>;
export const RemoveSkillDocument = new TypedDocumentString(`
    mutation RemoveSkill($name: String!) {
  removeSkill(name: $name) {
    id
    name
  }
}
    `) as unknown as TypedDocumentString<RemoveSkillMutation, RemoveSkillMutationVariables>;
export const SkillChangedDocument = new TypedDocumentString(`
    subscription SkillChanged {
  skillChanged {
    action
    skillId
    name
  }
}
    `) as unknown as TypedDocumentString<SkillChangedSubscription, SkillChangedSubscriptionVariables>;
export const TaskDocument = new TypedDocumentString(`
    query Task($id: ID!) {
  task(id: $id) {
    ...TaskFields
  }
}
    fragment TaskFields on Task {
  id
  operationId
  name
  description
  riskScore
  riskDescription
  profitScore
  profitDescription
  stage
  status
  summary
  assignees {
    id
    username
  }
  wikiReferences {
    id
    title
    emoji
  }
  credentialReferences {
    id
    name
    type
  }
  createdBy {
    id
    username
  }
  lastUpdatedBy {
    id
    username
  }
  lastUpdatedAt
  deletedAt
  doneAt
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<TaskQuery, TaskQueryVariables>;
export const TasksDocument = new TypedDocumentString(`
    query Tasks($operationId: ID!, $stage: TaskStage, $excludeStages: [TaskStage!], $riskScoreMin: Int, $riskScoreMax: Int, $profitScoreMin: Int, $profitScoreMax: Int, $search: String, $first: Int, $after: String) {
  tasks(
    operationId: $operationId
    stage: $stage
    excludeStages: $excludeStages
    riskScoreMin: $riskScoreMin
    riskScoreMax: $riskScoreMax
    profitScoreMin: $profitScoreMin
    profitScoreMax: $profitScoreMax
    search: $search
    first: $first
    after: $after
  ) {
    edges {
      node {
        ...TaskFields
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
    fragment TaskFields on Task {
  id
  operationId
  name
  description
  riskScore
  riskDescription
  profitScore
  profitDescription
  stage
  status
  summary
  assignees {
    id
    username
  }
  wikiReferences {
    id
    title
    emoji
  }
  credentialReferences {
    id
    name
    type
  }
  createdBy {
    id
    username
  }
  lastUpdatedBy {
    id
    username
  }
  lastUpdatedAt
  deletedAt
  doneAt
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<TasksQuery, TasksQueryVariables>;
export const TaskTrashDocument = new TypedDocumentString(`
    query TaskTrash($operationId: ID!, $first: Int, $after: String) {
  taskTrash(operationId: $operationId, first: $first, after: $after) {
    edges {
      node {
        ...TaskFields
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
    fragment TaskFields on Task {
  id
  operationId
  name
  description
  riskScore
  riskDescription
  profitScore
  profitDescription
  stage
  status
  summary
  assignees {
    id
    username
  }
  wikiReferences {
    id
    title
    emoji
  }
  credentialReferences {
    id
    name
    type
  }
  createdBy {
    id
    username
  }
  lastUpdatedBy {
    id
    username
  }
  lastUpdatedAt
  deletedAt
  doneAt
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<TaskTrashQuery, TaskTrashQueryVariables>;
export const TasksReferencingWikiDocumentDocument = new TypedDocumentString(`
    query TasksReferencingWikiDocument($documentId: ID!) {
  tasksReferencingWikiDocument(documentId: $documentId) {
    ...TaskBacklinkFields
  }
}
    fragment TaskBacklinkFields on Task {
  id
  operationId
  name
  stage
  status
  riskScore
  profitScore
  assignees {
    id
    username
  }
}`) as unknown as TypedDocumentString<TasksReferencingWikiDocumentQuery, TasksReferencingWikiDocumentQueryVariables>;
export const TasksReferencingCredentialDocument = new TypedDocumentString(`
    query TasksReferencingCredential($credentialId: ID!) {
  tasksReferencingCredential(credentialId: $credentialId) {
    ...TaskBacklinkFields
  }
}
    fragment TaskBacklinkFields on Task {
  id
  operationId
  name
  stage
  status
  riskScore
  profitScore
  assignees {
    id
    username
  }
}`) as unknown as TypedDocumentString<TasksReferencingCredentialQuery, TasksReferencingCredentialQueryVariables>;
export const CreateTaskDocument = new TypedDocumentString(`
    mutation CreateTask($input: CreateTaskInput!) {
  createTask(input: $input) {
    ...TaskFields
  }
}
    fragment TaskFields on Task {
  id
  operationId
  name
  description
  riskScore
  riskDescription
  profitScore
  profitDescription
  stage
  status
  summary
  assignees {
    id
    username
  }
  wikiReferences {
    id
    title
    emoji
  }
  credentialReferences {
    id
    name
    type
  }
  createdBy {
    id
    username
  }
  lastUpdatedBy {
    id
    username
  }
  lastUpdatedAt
  deletedAt
  doneAt
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<CreateTaskMutation, CreateTaskMutationVariables>;
export const UpdateTaskDocument = new TypedDocumentString(`
    mutation UpdateTask($id: ID!, $input: UpdateTaskInput!) {
  updateTask(id: $id, input: $input) {
    ...TaskFields
  }
}
    fragment TaskFields on Task {
  id
  operationId
  name
  description
  riskScore
  riskDescription
  profitScore
  profitDescription
  stage
  status
  summary
  assignees {
    id
    username
  }
  wikiReferences {
    id
    title
    emoji
  }
  credentialReferences {
    id
    name
    type
  }
  createdBy {
    id
    username
  }
  lastUpdatedBy {
    id
    username
  }
  lastUpdatedAt
  deletedAt
  doneAt
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<UpdateTaskMutation, UpdateTaskMutationVariables>;
export const ChangeTaskStageDocument = new TypedDocumentString(`
    mutation ChangeTaskStage($input: ChangeTaskStageInput!) {
  changeTaskStage(input: $input) {
    ...TaskFields
  }
}
    fragment TaskFields on Task {
  id
  operationId
  name
  description
  riskScore
  riskDescription
  profitScore
  profitDescription
  stage
  status
  summary
  assignees {
    id
    username
  }
  wikiReferences {
    id
    title
    emoji
  }
  credentialReferences {
    id
    name
    type
  }
  createdBy {
    id
    username
  }
  lastUpdatedBy {
    id
    username
  }
  lastUpdatedAt
  deletedAt
  doneAt
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<ChangeTaskStageMutation, ChangeTaskStageMutationVariables>;
export const SetTaskAssigneesDocument = new TypedDocumentString(`
    mutation SetTaskAssignees($taskId: ID!, $assigneeIds: [ID!]!) {
  setTaskAssignees(taskId: $taskId, assigneeIds: $assigneeIds) {
    ...TaskFields
  }
}
    fragment TaskFields on Task {
  id
  operationId
  name
  description
  riskScore
  riskDescription
  profitScore
  profitDescription
  stage
  status
  summary
  assignees {
    id
    username
  }
  wikiReferences {
    id
    title
    emoji
  }
  credentialReferences {
    id
    name
    type
  }
  createdBy {
    id
    username
  }
  lastUpdatedBy {
    id
    username
  }
  lastUpdatedAt
  deletedAt
  doneAt
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<SetTaskAssigneesMutation, SetTaskAssigneesMutationVariables>;
export const SetTaskWikiReferencesDocument = new TypedDocumentString(`
    mutation SetTaskWikiReferences($taskId: ID!, $wikiIds: [ID!]!) {
  setTaskWikiReferences(taskId: $taskId, wikiIds: $wikiIds) {
    ...TaskFields
  }
}
    fragment TaskFields on Task {
  id
  operationId
  name
  description
  riskScore
  riskDescription
  profitScore
  profitDescription
  stage
  status
  summary
  assignees {
    id
    username
  }
  wikiReferences {
    id
    title
    emoji
  }
  credentialReferences {
    id
    name
    type
  }
  createdBy {
    id
    username
  }
  lastUpdatedBy {
    id
    username
  }
  lastUpdatedAt
  deletedAt
  doneAt
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<SetTaskWikiReferencesMutation, SetTaskWikiReferencesMutationVariables>;
export const AddTaskWikiReferenceDocument = new TypedDocumentString(`
    mutation AddTaskWikiReference($taskId: ID!, $wikiId: ID!) {
  addTaskWikiReference(taskId: $taskId, wikiId: $wikiId) {
    ...TaskFields
  }
}
    fragment TaskFields on Task {
  id
  operationId
  name
  description
  riskScore
  riskDescription
  profitScore
  profitDescription
  stage
  status
  summary
  assignees {
    id
    username
  }
  wikiReferences {
    id
    title
    emoji
  }
  credentialReferences {
    id
    name
    type
  }
  createdBy {
    id
    username
  }
  lastUpdatedBy {
    id
    username
  }
  lastUpdatedAt
  deletedAt
  doneAt
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<AddTaskWikiReferenceMutation, AddTaskWikiReferenceMutationVariables>;
export const SetTaskCredentialReferencesDocument = new TypedDocumentString(`
    mutation SetTaskCredentialReferences($taskId: ID!, $credentialIds: [ID!]!) {
  setTaskCredentialReferences(taskId: $taskId, credentialIds: $credentialIds) {
    ...TaskFields
  }
}
    fragment TaskFields on Task {
  id
  operationId
  name
  description
  riskScore
  riskDescription
  profitScore
  profitDescription
  stage
  status
  summary
  assignees {
    id
    username
  }
  wikiReferences {
    id
    title
    emoji
  }
  credentialReferences {
    id
    name
    type
  }
  createdBy {
    id
    username
  }
  lastUpdatedBy {
    id
    username
  }
  lastUpdatedAt
  deletedAt
  doneAt
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<SetTaskCredentialReferencesMutation, SetTaskCredentialReferencesMutationVariables>;
export const DeleteTaskDocument = new TypedDocumentString(`
    mutation DeleteTask($id: ID!) {
  deleteTask(id: $id)
}
    `) as unknown as TypedDocumentString<DeleteTaskMutation, DeleteTaskMutationVariables>;
export const RestoreTaskDocument = new TypedDocumentString(`
    mutation RestoreTask($id: ID!) {
  restoreTask(id: $id) {
    ...TaskFields
  }
}
    fragment TaskFields on Task {
  id
  operationId
  name
  description
  riskScore
  riskDescription
  profitScore
  profitDescription
  stage
  status
  summary
  assignees {
    id
    username
  }
  wikiReferences {
    id
    title
    emoji
  }
  credentialReferences {
    id
    name
    type
  }
  createdBy {
    id
    username
  }
  lastUpdatedBy {
    id
    username
  }
  lastUpdatedAt
  deletedAt
  doneAt
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<RestoreTaskMutation, RestoreTaskMutationVariables>;
export const PurgeTaskDocument = new TypedDocumentString(`
    mutation PurgeTask($id: ID!) {
  purgeTask(id: $id)
}
    `) as unknown as TypedDocumentString<PurgeTaskMutation, PurgeTaskMutationVariables>;
export const TaskChangedDocument = new TypedDocumentString(`
    subscription TaskChanged($operationId: ID!) {
  taskChanged(operationId: $operationId) {
    action
    taskId
    operationId
    task {
      ...TaskFields
    }
  }
}
    fragment TaskFields on Task {
  id
  operationId
  name
  description
  riskScore
  riskDescription
  profitScore
  profitDescription
  stage
  status
  summary
  assignees {
    id
    username
  }
  wikiReferences {
    id
    title
    emoji
  }
  credentialReferences {
    id
    name
    type
  }
  createdBy {
    id
    username
  }
  lastUpdatedBy {
    id
    username
  }
  lastUpdatedAt
  deletedAt
  doneAt
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<TaskChangedSubscription, TaskChangedSubscriptionVariables>;
export const TimelineBucketsDocument = new TypedDocumentString(`
    query TimelineBuckets($operationId: ID!, $granularity: TimelineGranularity = DAY, $timezone: String!, $from: String, $to: String, $types: [String!], $actorIds: [ID!]) {
  timelineBuckets(
    operationId: $operationId
    granularity: $granularity
    timezone: $timezone
    from: $from
    to: $to
    types: $types
    actorIds: $actorIds
  ) {
    bucketStart
    count
    topicCounts {
      topic
      subjectKind
      count
      emoji
      icon
      color
    }
  }
}
    `) as unknown as TypedDocumentString<TimelineBucketsQuery, TimelineBucketsQueryVariables>;
export const TimelineEventsByDayDocument = new TypedDocumentString(`
    query TimelineEventsByDay($operationId: ID!, $date: String!, $timezone: String!, $granularity: TimelineGranularity = DAY, $types: [String!], $actorIds: [ID!], $first: Int = 100, $after: String) {
  timelineEventsByDay(
    operationId: $operationId
    date: $date
    timezone: $timezone
    granularity: $granularity
    types: $types
    actorIds: $actorIds
    first: $first
    after: $after
  ) {
    edges {
      node {
        ...TimelineEventFields
      }
      cursor
    }
    pageInfo {
      hasNextPage
      hasPreviousPage
      startCursor
      endCursor
    }
  }
}
    fragment TimelineEventFields on TimelineEvent {
  id
  operationId
  topic
  subjectKind
  subjectId
  subjectName
  occurredAt
  metadata
  actor {
    id
    username
  }
  actorKind
  actorLabel
}`) as unknown as TypedDocumentString<TimelineEventsByDayQuery, TimelineEventsByDayQueryVariables>;
export const TimelineEventAddedDocument = new TypedDocumentString(`
    subscription TimelineEventAdded($operationId: ID!) {
  timelineEventAdded(operationId: $operationId) {
    ...TimelineEventFields
  }
}
    fragment TimelineEventFields on TimelineEvent {
  id
  operationId
  topic
  subjectKind
  subjectId
  subjectName
  occurredAt
  metadata
  actor {
    id
    username
  }
  actorKind
  actorLabel
}`) as unknown as TypedDocumentString<TimelineEventAddedSubscription, TimelineEventAddedSubscriptionVariables>;
export const CreateCustomTimelineEventDocument = new TypedDocumentString(`
    mutation CreateCustomTimelineEvent($operationId: ID!, $input: CreateCustomTimelineEventInput!) {
  createCustomTimelineEvent(operationId: $operationId, input: $input) {
    ...TimelineEventFields
  }
}
    fragment TimelineEventFields on TimelineEvent {
  id
  operationId
  topic
  subjectKind
  subjectId
  subjectName
  occurredAt
  metadata
  actor {
    id
    username
  }
  actorKind
  actorLabel
}`) as unknown as TypedDocumentString<CreateCustomTimelineEventMutation, CreateCustomTimelineEventMutationVariables>;
export const UpdateCustomTimelineEventDocument = new TypedDocumentString(`
    mutation UpdateCustomTimelineEvent($id: ID!, $input: UpdateCustomTimelineEventInput!) {
  updateCustomTimelineEvent(id: $id, input: $input) {
    ...TimelineEventFields
  }
}
    fragment TimelineEventFields on TimelineEvent {
  id
  operationId
  topic
  subjectKind
  subjectId
  subjectName
  occurredAt
  metadata
  actor {
    id
    username
  }
  actorKind
  actorLabel
}`) as unknown as TypedDocumentString<UpdateCustomTimelineEventMutation, UpdateCustomTimelineEventMutationVariables>;
export const DeleteCustomTimelineEventDocument = new TypedDocumentString(`
    mutation DeleteCustomTimelineEvent($id: ID!) {
  deleteCustomTimelineEvent(id: $id)
}
    `) as unknown as TypedDocumentString<DeleteCustomTimelineEventMutation, DeleteCustomTimelineEventMutationVariables>;
export const MeDocument = new TypedDocumentString(`
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
    fragment UserFields on User {
  id
  username
  roles
  active
  authSource
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<MeQuery, MeQueryVariables>;
export const CompleteGuideDocument = new TypedDocumentString(`
    mutation CompleteGuide($guide: String!) {
  completeGuide(guide: $guide) {
    id
    completedGuides
  }
}
    `) as unknown as TypedDocumentString<CompleteGuideMutation, CompleteGuideMutationVariables>;
export const RecordIconUseDocument = new TypedDocumentString(`
    mutation RecordIconUse($name: String!) {
  recordIconUse(name: $name) {
    id
    frequentIcons
  }
}
    `) as unknown as TypedDocumentString<RecordIconUseMutation, RecordIconUseMutationVariables>;
export const TouchRecentOperationDocument = new TypedDocumentString(`
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
    `) as unknown as TypedDocumentString<TouchRecentOperationMutation, TouchRecentOperationMutationVariables>;
export const ImportLocalPreferencesDocument = new TypedDocumentString(`
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
    `) as unknown as TypedDocumentString<ImportLocalPreferencesMutation, ImportLocalPreferencesMutationVariables>;
export const UserDocument = new TypedDocumentString(`
    query User($id: ID!) {
  user(id: $id) {
    ...UserFields
  }
}
    fragment UserFields on User {
  id
  username
  roles
  active
  authSource
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<UserQuery, UserQueryVariables>;
export const UsersDocument = new TypedDocumentString(`
    query Users($search: String, $sortBy: UserSortField, $sortDirection: SortDirection, $first: Int, $after: String) {
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
    fragment UserFields on User {
  id
  username
  roles
  active
  authSource
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<UsersQuery, UsersQueryVariables>;
export const CreateUserDocument = new TypedDocumentString(`
    mutation CreateUser($input: CreateUserInput!) {
  createUser(input: $input) {
    ...UserFields
  }
}
    fragment UserFields on User {
  id
  username
  roles
  active
  authSource
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<CreateUserMutation, CreateUserMutationVariables>;
export const UpdateUserDocument = new TypedDocumentString(`
    mutation UpdateUser($id: ID!, $input: UpdateUserInput!) {
  updateUser(id: $id, input: $input) {
    ...UserFields
  }
}
    fragment UserFields on User {
  id
  username
  roles
  active
  authSource
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<UpdateUserMutation, UpdateUserMutationVariables>;
export const DeleteUserDocument = new TypedDocumentString(`
    mutation DeleteUser($id: ID!) {
  deleteUser(id: $id)
}
    `) as unknown as TypedDocumentString<DeleteUserMutation, DeleteUserMutationVariables>;
export const UpdateOwnProfileDocument = new TypedDocumentString(`
    mutation UpdateOwnProfile($input: UpdateUserInput!) {
  updateOwnProfile(input: $input) {
    ...UserFields
  }
}
    fragment UserFields on User {
  id
  username
  roles
  active
  authSource
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<UpdateOwnProfileMutation, UpdateOwnProfileMutationVariables>;
export const SetHiddenIdentitiesDocument = new TypedDocumentString(`
    mutation SetHiddenIdentities($names: [String!]!) {
  setHiddenIdentities(names: $names) {
    id
    hiddenIdentities
  }
}
    `) as unknown as TypedDocumentString<SetHiddenIdentitiesMutation, SetHiddenIdentitiesMutationVariables>;
export const UserChangedDocument = new TypedDocumentString(`
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
    fragment UserFields on User {
  id
  username
  roles
  active
  authSource
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<UserChangedSubscription, UserChangedSubscriptionVariables>;
export const WikiDocumentTreeDocument = new TypedDocumentString(`
    query WikiDocumentTree($operationId: ID!) {
  wikiDocumentTree(operationId: $operationId) {
    ...WikiDocumentTreeFields
  }
}
    fragment WikiDocumentTreeFields on WikiDocument {
  id
  operationId
  parentDocumentId
  title
  emoji
  icon
  color
  sortOrder
  childCount
  hasContent
  kind
  isTemplate
  sourceTemplateId
  checklistTotal
  checklistRequired
  checklistAnswered
  pageType
  tags
  status
  lastUpdatedAt
  updatedAt
}`) as unknown as TypedDocumentString<WikiDocumentTreeQuery, WikiDocumentTreeQueryVariables>;
export const WikiTemplatesDocument = new TypedDocumentString(`
    query WikiTemplates($operationId: ID!) {
  wikiTemplates(operationId: $operationId) {
    ...WikiDocumentTreeFields
  }
}
    fragment WikiDocumentTreeFields on WikiDocument {
  id
  operationId
  parentDocumentId
  title
  emoji
  icon
  color
  sortOrder
  childCount
  hasContent
  kind
  isTemplate
  sourceTemplateId
  checklistTotal
  checklistRequired
  checklistAnswered
  pageType
  tags
  status
  lastUpdatedAt
  updatedAt
}`) as unknown as TypedDocumentString<WikiTemplatesQuery, WikiTemplatesQueryVariables>;
export const WikiDocumentMarkdownDocument = new TypedDocumentString(`
    query WikiDocumentMarkdown($id: ID!) {
  wikiDocumentMarkdown(id: $id)
}
    `) as unknown as TypedDocumentString<WikiDocumentMarkdownQuery, WikiDocumentMarkdownQueryVariables>;
export const WikiDrawingSceneDocument = new TypedDocumentString(`
    query WikiDrawingScene($id: ID!) {
  wikiDrawingScene(id: $id)
}
    `) as unknown as TypedDocumentString<WikiDrawingSceneQuery, WikiDrawingSceneQueryVariables>;
export const WikiDocumentChildrenDocument = new TypedDocumentString(`
    query WikiDocumentChildren($operationId: ID!, $parentDocumentId: ID) {
  wikiDocumentChildren(
    operationId: $operationId
    parentDocumentId: $parentDocumentId
  ) {
    ...WikiDocumentTreeFields
  }
}
    fragment WikiDocumentTreeFields on WikiDocument {
  id
  operationId
  parentDocumentId
  title
  emoji
  icon
  color
  sortOrder
  childCount
  hasContent
  kind
  isTemplate
  sourceTemplateId
  checklistTotal
  checklistRequired
  checklistAnswered
  pageType
  tags
  status
  lastUpdatedAt
  updatedAt
}`) as unknown as TypedDocumentString<WikiDocumentChildrenQuery, WikiDocumentChildrenQueryVariables>;
export const WikiDocumentTreeRevealPathDocument = new TypedDocumentString(`
    query WikiDocumentTreeRevealPath($documentId: ID!) {
  wikiDocumentTreeRevealPath(documentId: $documentId) {
    ...WikiDocumentTreeFields
  }
}
    fragment WikiDocumentTreeFields on WikiDocument {
  id
  operationId
  parentDocumentId
  title
  emoji
  icon
  color
  sortOrder
  childCount
  hasContent
  kind
  isTemplate
  sourceTemplateId
  checklistTotal
  checklistRequired
  checklistAnswered
  pageType
  tags
  status
  lastUpdatedAt
  updatedAt
}`) as unknown as TypedDocumentString<WikiDocumentTreeRevealPathQuery, WikiDocumentTreeRevealPathQueryVariables>;
export const WikiDocumentDescendantIdsDocument = new TypedDocumentString(`
    query WikiDocumentDescendantIds($documentId: ID!) {
  wikiDocumentDescendantIds(documentId: $documentId)
}
    `) as unknown as TypedDocumentString<WikiDocumentDescendantIdsQuery, WikiDocumentDescendantIdsQueryVariables>;
export const WikiDocumentTrashCountDocument = new TypedDocumentString(`
    query WikiDocumentTrashCount($operationId: ID!) {
  wikiDocumentTrashCount(operationId: $operationId)
}
    `) as unknown as TypedDocumentString<WikiDocumentTrashCountQuery, WikiDocumentTrashCountQueryVariables>;
export const WikiDocumentDocument = new TypedDocumentString(`
    query WikiDocument($id: ID!) {
  wikiDocument(id: $id) {
    ...WikiDocumentFields
  }
}
    fragment WikiDocumentFields on WikiDocument {
  id
  operationId
  parentDocumentId
  ancestors {
    id
    title
    emoji
    icon
    color
    kind
    isDeleted
  }
  title
  kind
  content
  emoji
  color
  icon
  sortOrder
  isTemplate
  sourceTemplateId
  checklistTotal
  checklistRequired
  checklistAnswered
  pageType
  tags
  status
  createdBy {
    id
    username
  }
  lastUpdatedBy {
    id
    username
  }
  lastUpdatedAt
  lastBackupAt
  createdAt
  updatedAt
}`) as unknown as TypedDocumentString<WikiDocumentQuery, WikiDocumentQueryVariables>;
export const WikiRecentDocumentsDocument = new TypedDocumentString(`
    query WikiRecentDocuments($operationId: ID!, $sort: WikiDocumentSort, $first: Int, $after: String) {
  wikiDocuments(
    operationId: $operationId
    sort: $sort
    first: $first
    after: $after
  ) {
    edges {
      node {
        id
        title
        emoji
        icon
        color
        kind
        parentDocumentId
        ancestors {
          id
          title
          emoji
          icon
          color
          kind
          isDeleted
        }
        createdAt
        updatedAt
        lastUpdatedAt
        createdBy {
          id
          username
        }
        lastUpdatedBy {
          id
          username
        }
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
    `) as unknown as TypedDocumentString<WikiRecentDocumentsQuery, WikiRecentDocumentsQueryVariables>;
export const WikiSearchDocument = new TypedDocumentString(`
    query WikiSearch($operationId: ID!, $scope: ID, $query: String!, $offset: Int, $limit: Int) {
  wikiSearch(
    operationId: $operationId
    scope: $scope
    query: $query
    offset: $offset
    limit: $limit
  ) {
    hits {
      document {
        id
        title
        emoji
        icon
        color
        kind
        parentDocumentId
        ancestors {
          id
          title
          emoji
          icon
          color
          kind
          isDeleted
        }
        createdBy {
          id
          username
        }
      }
      snippet
      matchRanges {
        start
        end
      }
      score
    }
    total
    hasMore
  }
}
    `) as unknown as TypedDocumentString<WikiSearchQuery, WikiSearchQueryVariables>;
export const WikiDocumentLiteDocument = new TypedDocumentString(`
    query WikiDocumentLite($id: ID!) {
  wikiDocument(id: $id) {
    ...WikiDocumentLiteFields
  }
}
    fragment WikiDocumentLiteFields on WikiDocument {
  id
  title
  emoji
  icon
  color
  kind
  isTemplate
  deletedAt
}`) as unknown as TypedDocumentString<WikiDocumentLiteQuery, WikiDocumentLiteQueryVariables>;
export const WikiDocumentPreviewDocument = new TypedDocumentString(`
    query WikiDocumentPreview($id: ID!, $excerptLength: Int) {
  wikiDocument(id: $id) {
    ...WikiDocumentLiteFields
    excerpt(maxLength: $excerptLength)
    hasContent
    kind
    childCount
    updatedAt
    ancestors {
      id
      title
      emoji
      icon
      color
      isDeleted
    }
  }
}
    fragment WikiDocumentLiteFields on WikiDocument {
  id
  title
  emoji
  icon
  color
  kind
  isTemplate
  deletedAt
}`) as unknown as TypedDocumentString<WikiDocumentPreviewQuery, WikiDocumentPreviewQueryVariables>;
export const WikiDocumentBacklinksDocument = new TypedDocumentString(`
    query WikiDocumentBacklinks($documentId: ID!) {
  wikiDocumentBacklinks(documentId: $documentId) {
    ...WikiDocumentBacklinkFields
  }
}
    fragment WikiDocumentBacklinkFields on WikiDocument {
  id
  title
  emoji
  icon
  color
  kind
  updatedAt
  ancestors {
    id
    title
    emoji
    icon
    color
    kind
    isDeleted
  }
}`) as unknown as TypedDocumentString<WikiDocumentBacklinksQuery, WikiDocumentBacklinksQueryVariables>;
export const WikiDocumentTrashDocument = new TypedDocumentString(`
    query WikiDocumentTrash($operationId: ID!, $first: Int, $after: String) {
  wikiDocumentTrash(operationId: $operationId, first: $first, after: $after) {
    edges {
      node {
        id
        title
        emoji
        icon
        color
        kind
        deletedAt
        deletedBy {
          id
          username
        }
        createdAt
        ancestors {
          id
          title
          emoji
          icon
          color
          kind
          isDeleted
        }
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
    `) as unknown as TypedDocumentString<WikiDocumentTrashQuery, WikiDocumentTrashQueryVariables>;
export const WikiDocumentBackupsDocument = new TypedDocumentString(`
    query WikiDocumentBackups($documentId: ID!, $first: Int, $after: String) {
  wikiDocumentBackups(documentId: $documentId, first: $first, after: $after) {
    edges {
      node {
        ...WikiDocumentBackupListFields
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
    fragment WikiDocumentBackupListFields on WikiDocumentBackup {
  id
  documentId
  title
  trigger
  description
  contentLength
  createdBy {
    id
    username
  }
  createdAt
}`) as unknown as TypedDocumentString<WikiDocumentBackupsQuery, WikiDocumentBackupsQueryVariables>;
export const WikiDocumentBackupDetailDocument = new TypedDocumentString(`
    query WikiDocumentBackupDetail($id: ID!) {
  wikiDocumentBackup(id: $id) {
    ...WikiDocumentBackupDetailFields
  }
}
    fragment WikiDocumentBackupDetailFields on WikiDocumentBackup {
  id
  documentId
  title
  kind
  content
  contentLength
  trigger
  description
  createdBy {
    id
    username
  }
  createdAt
}`) as unknown as TypedDocumentString<WikiDocumentBackupDetailQuery, WikiDocumentBackupDetailQueryVariables>;
export const WikiDocumentPresenceDocument = new TypedDocumentString(`
    query WikiDocumentPresence($documentId: ID!) {
  wikiDocumentPresence(documentId: $documentId) {
    documentId
    activeEditors {
      userId
      username
      connectedAt
    }
  }
}
    `) as unknown as TypedDocumentString<WikiDocumentPresenceQuery, WikiDocumentPresenceQueryVariables>;
export const WikiOperationPresenceDocument = new TypedDocumentString(`
    query WikiOperationPresence($operationId: ID!) {
  wikiOperationPresence(operationId: $operationId) {
    documentId
    activeEditors {
      userId
      username
      connectedAt
    }
  }
}
    `) as unknown as TypedDocumentString<WikiOperationPresenceQuery, WikiOperationPresenceQueryVariables>;
export const WikiDocumentHistoryDocument = new TypedDocumentString(`
    query WikiDocumentHistory($operationId: ID!, $offset: Int, $limit: Int) {
  wikiDocumentHistory(operationId: $operationId, offset: $offset, limit: $limit) {
    edges {
      node {
        ...WikiDocumentVisitListFields
      }
    }
    totalCount
  }
}
    fragment WikiDocumentVisitListFields on WikiDocumentVisit {
  id
  visitedAt
  document {
    id
    title
    emoji
    icon
    color
    kind
    ancestors {
      id
      title
      emoji
      icon
      color
      kind
      isDeleted
    }
  }
}`) as unknown as TypedDocumentString<WikiDocumentHistoryQuery, WikiDocumentHistoryQueryVariables>;
export const WikiDocumentTagsDocument = new TypedDocumentString(`
    query WikiDocumentTags($operationId: ID!) {
  wikiDocumentTags(operationId: $operationId) {
    tag
    count
  }
}
    `) as unknown as TypedDocumentString<WikiDocumentTagsQuery, WikiDocumentTagsQueryVariables>;
export const WikiDocumentPageTypesDocument = new TypedDocumentString(`
    query WikiDocumentPageTypes($operationId: ID!) {
  wikiDocumentPageTypes(operationId: $operationId) {
    pageType
    count
  }
}
    `) as unknown as TypedDocumentString<WikiDocumentPageTypesQuery, WikiDocumentPageTypesQueryVariables>;
export const CreateWikiDocumentDocument = new TypedDocumentString(`
    mutation CreateWikiDocument($operationId: ID!, $input: CreateWikiDocumentInput!) {
  createWikiDocument(operationId: $operationId, input: $input) {
    id
    operationId
    title
    emoji
    color
    icon
    sortOrder
    parentDocumentId
    pageType
    tags
    status
    createdBy {
      id
      username
    }
    createdAt
    updatedAt
  }
}
    `) as unknown as TypedDocumentString<CreateWikiDocumentMutation, CreateWikiDocumentMutationVariables>;
export const UpdateWikiDocumentDocument = new TypedDocumentString(`
    mutation UpdateWikiDocument($id: ID!, $input: UpdateWikiDocumentInput!) {
  updateWikiDocument(id: $id, input: $input) {
    id
    title
    emoji
    color
    icon
    sortOrder
    parentDocumentId
    pageType
    tags
    status
    updatedAt
  }
}
    `) as unknown as TypedDocumentString<UpdateWikiDocumentMutation, UpdateWikiDocumentMutationVariables>;
export const ReorderWikiDocumentSiblingsDocument = new TypedDocumentString(`
    mutation ReorderWikiDocumentSiblings($input: ReorderWikiDocumentSiblingsInput!) {
  reorderWikiDocumentSiblings(input: $input) {
    id
    sortOrder
    parentDocumentId
    updatedAt
  }
}
    `) as unknown as TypedDocumentString<ReorderWikiDocumentSiblingsMutation, ReorderWikiDocumentSiblingsMutationVariables>;
export const DeleteWikiDocumentDocument = new TypedDocumentString(`
    mutation DeleteWikiDocument($id: ID!) {
  deleteWikiDocument(id: $id)
}
    `) as unknown as TypedDocumentString<DeleteWikiDocumentMutation, DeleteWikiDocumentMutationVariables>;
export const DuplicateWikiDocumentDocument = new TypedDocumentString(`
    mutation DuplicateWikiDocument($id: ID!, $withChildren: Boolean) {
  duplicateWikiDocument(id: $id, withChildren: $withChildren) {
    id
    operationId
    title
    emoji
    color
    icon
    sortOrder
    parentDocumentId
    createdAt
    updatedAt
  }
}
    `) as unknown as TypedDocumentString<DuplicateWikiDocumentMutation, DuplicateWikiDocumentMutationVariables>;
export const SetWikiDocumentTemplateDocument = new TypedDocumentString(`
    mutation SetWikiDocumentTemplate($id: ID!, $isTemplate: Boolean!) {
  setWikiDocumentTemplate(id: $id, isTemplate: $isTemplate) {
    id
    operationId
    title
    emoji
    icon
    color
    sortOrder
    parentDocumentId
    isTemplate
    updatedAt
  }
}
    `) as unknown as TypedDocumentString<SetWikiDocumentTemplateMutation, SetWikiDocumentTemplateMutationVariables>;
export const InstantiateTemplateDocument = new TypedDocumentString(`
    mutation InstantiateTemplate($templateId: ID!, $targetOperationId: ID!, $parentDocumentId: ID, $title: String, $emoji: String, $icon: String, $color: String) {
  instantiateTemplate(
    templateId: $templateId
    targetOperationId: $targetOperationId
    parentDocumentId: $parentDocumentId
    title: $title
    emoji: $emoji
    icon: $icon
    color: $color
  ) {
    id
    operationId
    title
    emoji
    color
    icon
    sortOrder
    parentDocumentId
    isTemplate
    sourceTemplateId
    checklistTotal
    checklistRequired
    checklistAnswered
    createdAt
    updatedAt
  }
}
    `) as unknown as TypedDocumentString<InstantiateTemplateMutation, InstantiateTemplateMutationVariables>;
export const RestoreWikiDocumentDocument = new TypedDocumentString(`
    mutation RestoreWikiDocument($id: ID!, $cascade: Boolean) {
  restoreWikiDocument(id: $id, cascade: $cascade) {
    id
    operationId
    title
    emoji
    icon
    color
    sortOrder
    parentDocumentId
  }
}
    `) as unknown as TypedDocumentString<RestoreWikiDocumentMutation, RestoreWikiDocumentMutationVariables>;
export const WikiDocumentTrashedDescendantsDocument = new TypedDocumentString(`
    query WikiDocumentTrashedDescendants($documentId: ID!) {
  wikiDocumentTrashedDescendants(documentId: $documentId) {
    id
    title
    emoji
    icon
    color
    kind
  }
}
    `) as unknown as TypedDocumentString<WikiDocumentTrashedDescendantsQuery, WikiDocumentTrashedDescendantsQueryVariables>;
export const PermanentlyDeleteWikiDocumentDocument = new TypedDocumentString(`
    mutation PermanentlyDeleteWikiDocument($id: ID!) {
  permanentlyDeleteWikiDocument(id: $id)
}
    `) as unknown as TypedDocumentString<PermanentlyDeleteWikiDocumentMutation, PermanentlyDeleteWikiDocumentMutationVariables>;
export const EmptyWikiDocumentTrashDocument = new TypedDocumentString(`
    mutation EmptyWikiDocumentTrash($operationId: ID!) {
  emptyWikiDocumentTrash(operationId: $operationId)
}
    `) as unknown as TypedDocumentString<EmptyWikiDocumentTrashMutation, EmptyWikiDocumentTrashMutationVariables>;
export const CreateWikiDocumentBackupDocument = new TypedDocumentString(`
    mutation CreateWikiDocumentBackup($documentId: ID!, $description: String) {
  createWikiDocumentBackup(documentId: $documentId, description: $description) {
    id
    documentId
    title
    trigger
    description
    createdBy {
      id
      username
    }
    createdAt
  }
}
    `) as unknown as TypedDocumentString<CreateWikiDocumentBackupMutation, CreateWikiDocumentBackupMutationVariables>;
export const RestoreWikiDocumentBackupDocument = new TypedDocumentString(`
    mutation RestoreWikiDocumentBackup($documentId: ID!, $backupId: ID!) {
  restoreWikiDocumentBackup(documentId: $documentId, backupId: $backupId) {
    id
    title
    content
  }
}
    `) as unknown as TypedDocumentString<RestoreWikiDocumentBackupMutation, RestoreWikiDocumentBackupMutationVariables>;
export const DeleteWikiDocumentBackupDocument = new TypedDocumentString(`
    mutation DeleteWikiDocumentBackup($id: ID!) {
  deleteWikiDocumentBackup(id: $id)
}
    `) as unknown as TypedDocumentString<DeleteWikiDocumentBackupMutation, DeleteWikiDocumentBackupMutationVariables>;
export const TrackWikiDocumentVisitDocument = new TypedDocumentString(`
    mutation TrackWikiDocumentVisit($documentId: ID!) {
  trackWikiDocumentVisit(documentId: $documentId) {
    id
    visitedAt
  }
}
    `) as unknown as TypedDocumentString<TrackWikiDocumentVisitMutation, TrackWikiDocumentVisitMutationVariables>;
export const WikiDocumentChangedDocument = new TypedDocumentString(`
    subscription WikiDocumentChanged($operationId: ID!) {
  wikiDocumentChanged(operationId: $operationId) {
    action
    documentId
    operationId
    parentDocumentId
    previousParentDocumentId
  }
}
    `) as unknown as TypedDocumentString<WikiDocumentChangedSubscription, WikiDocumentChangedSubscriptionVariables>;
export const WikiDocumentPresenceChangedDocument = new TypedDocumentString(`
    subscription WikiDocumentPresenceChanged($operationId: ID!) {
  wikiDocumentPresenceChanged(operationId: $operationId) {
    documentId
    operationId
    userId
    username
    action
  }
}
    `) as unknown as TypedDocumentString<WikiDocumentPresenceChangedSubscription, WikiDocumentPresenceChangedSubscriptionVariables>;
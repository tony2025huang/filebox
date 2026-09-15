// 审计日志标签：后端在 audit_logs 里存的是稳定英文码（action/reason），前端只做查表翻译。
// 这里集中维护两份映射，LogsView 与 SharesView 共用，避免各写一份导致新码漏翻译（v044）。
// Audit log labels: the backend stores stable English codes for action/reason; the UI only
// translates them. Both maps live here so the two views cannot drift apart.

export const ACTION_LABEL_KEYS = {
  login: 'logs.login',
  register: 'logs.register',
  upload: 'logs.upload',
  upload_init: 'logs.uploadInit',
  upload_chunk: 'logs.uploadChunk',
  download: 'logs.download',
  delete: 'logs.delete',
  share: 'logs.share',
  share_view: 'logs.shareView',
  share_download: 'logs.shareDownload',
  share_preview: 'logs.sharePreview',
  share_extend: 'logs.shareExtend',
  share_increase: 'logs.shareIncrease',
  share_revoke: 'logs.shareRevoke',
  batch_share: 'logs.batchShare',
  share_group_extend: 'logs.shareGroupExtend',
  share_group_increase: 'logs.shareGroupIncrease',
  share_group_update: 'logs.shareGroupUpdate',
  collection: 'logs.collection',
  upload_collect: 'logs.uploadCollect',
  upload_collect_fail: 'logs.uploadCollectFail',
  collection_update: 'logs.collectionUpdate',
  clear_all: 'logs.clearAll',
  recycle_move: 'logs.recycleMove',
  recycle_purge: 'logs.recyclePurge',
  sync_run: 'logs.syncRun',
  sync_host_key: 'logs.syncHostKey',
  sync_host_key_update: 'logs.syncHostKeyUpdate',
  settings_update: 'logs.settingsUpdate',
  brand_update: 'logs.brandUpdate',
  language_update: 'logs.languageUpdate',
  password_change: 'logs.passwordChange',
  password_reset: 'logs.passwordReset',
  user_create: 'logs.userCreate',
  user_update: 'logs.userUpdate',
  user_disabled: 'logs.userDisabled',
  totp_update: 'logs.totpUpdate',
  ip_acl_update: 'logs.ipAclUpdate',
  folder_create: 'logs.folderCreate',
  folder_rename: 'logs.folderRename',
  folder_delete: 'logs.folderDelete',
  folder_list: 'logs.folderList',
  file_list: 'logs.fileList',
  admin_stats: 'logs.adminStats',
  log_list: 'logs.logList'
}

export const REASON_LABEL_KEYS = {
  // 登录与账号
  user_not_found: 'logReason.userNotFound',
  wrong_password: 'logReason.wrongPassword',
  user_disabled: 'logReason.userDisabled',
  locked: 'logReason.locked',
  ip_locked: 'logReason.ipLocked',
  totp_failed: 'logReason.totpFailed',
  register_disabled: 'logReason.registerDisabled',
  reauth_failed: 'logReason.reauthFailed',
  reauth_rate_limited: 'logReason.reauthRateLimited',
  unauthorized: 'logReason.unauthorized',
  rate_limited: 'logReason.rateLimited',
  read_only: 'logReason.readOnly',
  // 上传与下载
  not_found: 'logReason.notFound',
  content_not_found: 'logReason.contentNotFound',
  checksum_mismatch: 'logReason.checksumMismatch',
  save_failed: 'logReason.saveFailed',
  upload_failed: 'logReason.uploadFailed',
  invalid_name: 'logReason.invalidName',
  too_large: 'logReason.tooLarge',
  conflict: 'logReason.conflict',
  disk_full: 'logReason.diskFull',
  quota_exceeded: 'logReason.quotaExceeded',
  task_not_found: 'logReason.taskNotFound',
  invalid_index: 'logReason.invalidIndex',
  size_mismatch: 'logReason.sizeMismatch',
  invalid_dir: 'logReason.invalidDir',
  invalid_resolve: 'logReason.invalidResolve',
  prepare_failed: 'logReason.prepareFailed',
  conflict_check_failed: 'logReason.conflictCheckFailed',
  disk_check_failed: 'logReason.diskCheckFailed',
  task_create_failed: 'logReason.taskCreateFailed',
  invalid_chunk_size: 'logReason.invalidChunkSize',
  incomplete: 'logReason.incomplete',
  instant: 'logReason.instant',
  // 分享
  share_not_found: 'logReason.shareNotFound',
  share_expired: 'logReason.shareExpired',
  share_revoked: 'logReason.shareRevoked',
  share_limit: 'logReason.shareLimit',
  share_denied: 'logReason.shareDenied',
  share_content_missing: 'logReason.shareContentMissing',
  // 上传收集
  collection_upload: 'logReason.collectionUpload',
  collection_limit: 'logReason.collectionLimit',
  collection_file_too_large: 'logReason.collectionFileTooLarge',
  // 同步
  changed: 'logReason.hostKeyChanged',
  invalid_fingerprint: 'logReason.invalidFingerprint',
  // 管理与成功项的操作说明
  settings_failed: 'logReason.settingsFailed',
  invalid_request: 'logReason.invalidRequest',
  scope_forbidden: 'logReason.scopeForbidden',
  delete_failed: 'logReason.deleteFailed',
  write_failed: 'logReason.writeFailed',
  batch: 'logReason.batch',
  batch_group: 'logReason.batchGroup',
  clear_all: 'logReason.clearAll',
  create: 'logReason.create',
  revoke: 'logReason.revoke',
  revoke_group: 'logReason.revokeGroup',
  update: 'logReason.update',
  update_attributes: 'logReason.updateAttributes',
  add_files: 'logReason.addFiles',
  remove_file: 'logReason.removeFile',
  extend: 'logReason.extend',
  increase: 'logReason.increase',
  explicit_confirmation: 'logReason.explicitConfirmation',
  stale_confirmation: 'logReason.staleConfirmation',
  folder_delete: 'logReason.folderDelete',
  success: 'logReason.success'
}

export const ACTION_CODES = Object.keys(ACTION_LABEL_KEYS)
export const REASON_CODES = Object.keys(REASON_LABEL_KEYS)

// actionLabel 把操作码翻成当前语言；未知码（后端新增而前端未跟上）明确标注为待翻译，不再裸露英文。
// actionLabel translates an action code; unknown codes are explicitly marked as untranslated.
export function actionLabel(value, t) {
  const key = ACTION_LABEL_KEYS[value]
  if (key) return t(key)
  return value ? t('logs.unknownAction', { code: value }) : '-'
}

// reasonLabel 同上，用于原因码；空原因显示为占位符。
// reasonLabel does the same for reason codes; an empty reason renders as a placeholder.
export function reasonLabel(value, t) {
  const key = REASON_LABEL_KEYS[value]
  if (key) return t(key)
  return value ? t('logReason.unknown', { code: value }) : '-'
}

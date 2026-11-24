module.exports = {manual: [
  'intro',
  {type: 'category', label: '開始使用', collapsed: false, items: ['getting-started/installation', 'getting-started/quickstart']},
  {type: 'category', label: '循序教學', collapsed: false, items: ['tutorials/greeting-application', 'tutorials/bookstore', 'tutorials/transactions', 'tutorials/generated-bookstore']},
  'samples/index',
  {type: 'category', label: '基礎功能', items: ['fundamentals/modules', 'fundamentals/dependency-injection', 'fundamentals/configuration', 'fundamentals/application-startup']},
  {type: 'category', label: '資料存取', items: ['data/unit-of-work', 'data/database', 'data/postgresql', 'data/mysql', 'data/sqlite', 'data/idempotency', 'data/migrations']},
  {type: 'category', label: '身分與多租戶', items: ['security/authentication', 'security/multi-tenancy', 'security/authorization', 'security/resource-scopes', 'security/organizations', 'security/http-and-audit']},
  {type: 'category', label: '事件', items: ['events/integration-events', 'events/reliable-delivery']},
  {type: 'category', label: '設定、功能與快取', items: ['platform/settings', 'platform/features', 'platform/cache']},
  {type: 'category', label: '背景工作與維運', items: ['platform/jobs', 'platform/observability', 'platform/operations', 'platform/backup-restore']},
  'concepts/architecture',
  {type: 'category', label: '參考與疑難排解', items: ['reference/cli', 'reference/configuration', 'reference/bookstore-api', 'reference/compatibility', 'reference/upgrading', 'reference/troubleshooting']},
]};

module.exports = {manual: [
  'intro',
  {type: 'category', label: '開始使用', collapsed: false, items: ['getting-started/installation', 'getting-started/quickstart']},
  {type: 'category', label: '循序教學', collapsed: false, items: ['tutorials/greeting-application', 'tutorials/bookstore', 'tutorials/transactions']},
  'samples/index',
  {type: 'category', label: '基礎功能', items: ['fundamentals/modules', 'fundamentals/dependency-injection', 'fundamentals/configuration', 'fundamentals/application-startup']},
  {type: 'category', label: '資料存取', items: ['data/unit-of-work', 'data/postgresql', 'data/idempotency', 'data/migrations']},
  {type: 'category', label: '身分與多租戶', items: ['security/authentication', 'security/multi-tenancy', 'security/authorization', 'security/resource-scopes', 'security/organizations', 'security/http-and-audit']},
  {type: 'category', label: '事件', items: ['events/integration-events', 'events/reliable-delivery']},
  'concepts/architecture',
  {type: 'category', label: '參考與疑難排解', items: ['reference/configuration', 'reference/bookstore-api', 'reference/compatibility', 'reference/upgrading', 'reference/troubleshooting']},
]};

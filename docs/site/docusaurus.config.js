const {themes} = require('prism-react-renderer');
const {writeFileSync} = require('node:fs');
const path = require('node:path');
const {versionState} = require('./scripts/version.cjs');
const version = versionState();
const docsPath = `/docs/${version.lastVersion === 'current' ? version.currentPath : version.lastVersion}/`;
const baseUrl = process.env.DOCS_BASE_URL || '/';
if (!/^\/(?:[A-Za-z0-9_-]+\/)*$/.test(baseUrl)) {
  throw new Error('DOCS_BASE_URL must use simple path segments with leading/trailing slashes');
}

/** @type {import('@docusaurus/types').Config} */
module.exports = {
  title: 'panGOlin Framework',
  tagline: '用 Go，把應用的邊界寫清楚。',
  favicon: 'img/logo.svg',
  url: process.env.DOCS_URL || 'http://localhost:18088',
  baseUrl,
  trailingSlash: true,
  onBrokenLinks: 'throw',
  markdown: {hooks: {onBrokenMarkdownLinks: 'throw', onBrokenMarkdownImages: 'throw'}},
  i18n: {defaultLocale: 'zh-Hant', locales: ['zh-Hant']},
  customFields: {...version, docsPath},
  presets: [['classic', {
    docs: {
      path: 'content', routeBasePath: 'docs', sidebarPath: './sidebars.js',
      lastVersion: version.lastVersion,
      versions: {
        ...Object.fromEntries(version.released.map(name => [name, {label: name, path: name}])),
        current: {label: version.currentLabel, path: version.currentPath, banner: 'none'},
      },
    },
    blog: false,
    theme: {customCss: './src/css/custom.css'},
  }]],
  themes: [['@easyops-cn/docusaurus-search-local', {
    hashed: 'filename', language: ['en', 'zh'], docsDir: 'content',
    indexBlog: false, indexPages: true, highlightSearchTermsOnTargetPage: true,
    searchBarPosition: 'right', searchResultLimits: 8,
  }]],
  plugins: [function buildMetadata() {
    return {name: 'pangolin-build-metadata',
      configureWebpack() { return {module: {rules: [{test: /\.go$/, type: 'asset/source'}]}}; },
      async postBuild({outDir}) {
      writeFileSync(path.join(outDir, 'version-manifest.json'), JSON.stringify(version, null, 2) + '\n');
    }};
  }],
  themeConfig: {
    image: 'img/social-card.svg',
    colorMode: {defaultMode: 'light', respectPrefersColorScheme: false},
    navbar: {
      title: 'panGOlin', logo: {alt: 'panGOlin Framework', src: 'img/logo.svg'},
      items: [
        {type: 'docSidebar', sidebarId: 'manual', position: 'left', label: '文件'},
        {type: 'doc', docId: 'samples/index', label: '使用範例', position: 'left'},
        {type: 'docsVersionDropdown', position: 'right', dropdownItemsAfter: [{to: '/versions', label: '所有文件版本'}]},
        {href: 'https://github.com/cymondez/pangolin-framework', label: '原始碼', position: 'right'},
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {title: '開始使用', items: [{label: '安裝與建立專案', to: `${docsPath}getting-started/installation/`}, {label: '第一個應用', to: `${docsPath}getting-started/quickstart/`}]},
        {title: '範例與教學', items: [{label: '套件使用範例', to: `${docsPath}samples/`}, {label: 'Bookstore', to: `${docsPath}tutorials/bookstore/`}, {label: '文件版本', to: '/versions'}]},
        {title: '參與開發', items: [{label: 'GitHub', href: 'https://github.com/cymondez/pangolin-framework'}, {label: '回報問題', href: 'https://github.com/cymondez/pangolin-framework/issues'}]},
      ],
      copyright: `panGOlin Framework · ${version.productVersion} · Go 應用開發指南`,
    },
    prism: {
      theme: {...themes.github, plain: {color: '#2d3f50', backgroundColor: '#edf3f7'}},
      darkTheme: {...themes.nightOwl, plain: {color: '#e6edf3', backgroundColor: '#203344'}},
      additionalLanguages: ['go', 'powershell', 'bash', 'sql', 'docker', 'json', 'yaml'],
    },
    docs: {sidebar: {hideable: true, autoCollapseCategories: true}},
  },
};

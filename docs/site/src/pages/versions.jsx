import React from 'react';
import Layout from '@theme/Layout';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import {useAllDocsData} from '@docusaurus/plugin-content-docs/client';

export default function Versions() {
  const {siteConfig} = useDocusaurusContext();
  const {versions} = useAllDocsData().default;
  const {docsPath, productVersion} = siteConfig.customFields;
  return <Layout title="文件版本" description="選擇你正在使用的 panGOlin 套件版本，閱讀對應的安裝、教學與使用範例。">
    <main className="container pg-version-page">
      <div className="pg-eyebrow">DOCUMENTATION VERSIONS</div>
      <h1>選擇你的套件版本。</h1>
      <p className="pg-lead">使用與應用依賴相符的文件，取得正確的安裝方式、API 用法與範例。</p>
      <table><thead><tr><th>套件文件</th><th>說明</th><th>閱讀入口</th></tr></thead><tbody>{versions.map(version => <tr key={version.name}><td><strong>{version.label}</strong></td><td>{versions.length === 1 || version.name !== 'current' ? '對應正式套件的使用指南' : '開發中版本的使用指南'}</td><td><Link to={version.path}>閱讀文件</Link></td></tr>)}</tbody></table>
      {versions.length === 1 && <p>目前提供 v{productVersion} 正式版文件。未來發布新版本後，你可以在頁首切換並繼續閱讀舊版。</p>}
      <h2>第一次使用？</h2>
      <p>從<Link to={`${docsPath}getting-started/installation/`}>安裝與建立專案</Link>開始，再執行<Link to={`${docsPath}getting-started/quickstart/`}>第一個應用</Link>。想找某個功能的寫法，可以直接查看<Link to={`${docsPath}samples/`}>套件使用範例</Link>。</p>
      <h2>更新應用之前</h2>
      <p>確認<Link to={`${docsPath}reference/compatibility/`}>功能相容性</Link>，並參考<Link to={`${docsPath}reference/upgrading/`}>升級指南</Link>在自己的環境測試主要操作。</p>
    </main>
  </Layout>;
}

import React from 'react';
import Layout from '@theme/Layout';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import CodeBlock from '@theme/CodeBlock';

const topics = [
  {label: '組裝應用', title: '依賴與生命週期，由你明確定義。', text: '透過 Module、DI 與 Runtime，讓每個資源的建立、使用與清理都有擁有者。', path: 'fundamentals/modules', icon: '◈'},
  {label: '維護邊界', title: '在同一個工作單元內完成業務。', text: 'Required 交易、可信 Tenant Scope 與 PostgreSQL RLS，從入口一路約束到資料存取。', path: 'data/unit-of-work', icon: '▣'},
  {label: '傳遞變更', title: '事件先保存，再跨程序配送。', text: '沿著 Bookstore 的 Outbox、JetStream 與 Inbox，理解重送與處理去重的責任。', path: 'events/integration-events', icon: '⇌'},
];

export default function Home() {
  const {siteConfig} = useDocusaurusContext();
  const {docsPath, currentLabel} = siteConfig.customFields;
  return <Layout title="官方文件" description="從 Bookstore 開始，學習 panGOlin Framework 的模組、交易、安全邊界與可靠事件。">
    <main>
      <section className="pg-hero">
        <div className="container pg-hero-grid">
          <div>
            <div className="pg-eyebrow">GO MODULAR APPLICATION FRAMEWORK</div>
            <h1>用 Go，<br/>把應用的邊界<br/><em>寫清楚。</em></h1>
            <p className="pg-lead">從模組化單體開始，用明確的依賴、交易與租戶邊界，建立能持續演進的應用。</p>
            <div className="pg-actions"><Link className="button button--primary button--lg" to={`${docsPath}getting-started/installation/`}>開始使用</Link><Link className="button button--outline button--primary button--lg" to={`${docsPath}samples/`}>查看使用範例</Link></div>
            <p className="pg-version"><span className="pg-pill">{currentLabel}</span> 從完整程式開始，依需求加入功能</p>
          </div>
          <div className="pg-flow" aria-label="第一個 panGOlin 應用：執行程式、組裝服務並輸出問候訊息">
            <div className="pg-flow-top"><span className="pg-flow-dot"/> HELLO-PANGOLIN <span className="pg-flow-tag">GO</span></div>
            <div className="pg-flow-request"><span>YOUR APPLICATION</span><code>go run .</code><small>你的 main.go · 不需要資料庫</small></div>
            <div className="pg-flow-transaction"><div className="pg-flow-caption">PANGOLIN FRAMEWORK</div><div className="pg-flow-stores"><span>Module<small>組織功能</small></span><span className="pg-flow-plus">＋</span><span>Service<small>注入依賴</small></span></div><div className="pg-flow-commit">Hello, panGOlin!</div></div>
            <div className="pg-flow-line"/>
            <div className="pg-flow-events"><span>讀取設定</span><span>啟動應用</span><span>釋放資源</span></div>
            <div className="pg-flow-bottom">完整程式與預期輸出，照著做就能跑</div>
          </div>
        </div>
      </section>
      <section className="container pg-section">
        <div className="pg-section-heading"><div><div className="pg-eyebrow">BUILD YOUR APPLICATION, STEP BY STEP</div><h2>把需要的功能，加入你的應用。</h2></div><Link to={`${docsPath}concepts/architecture/`}>安排你的應用程式</Link></div>
        <div className="pg-topics">{topics.map(topic => <article className="pg-topic" key={topic.path}><div className="pg-topic-label"><span aria-hidden="true">{topic.icon}</span>{topic.label}</div><h3>{topic.title}</h3><p>{topic.text}</p><Link to={`${docsPath}${topic.path}/`}>閱讀教學</Link></article>)}</div>
      </section>
      <section className="container pg-section pg-start">
        <div><div className="pg-eyebrow">YOUR FIRST APPLICATION</div><h2>先跑出第一個應用，再依需求擴充。</h2><p>準備 Go、建立自己的專案，直接執行第一個模組。接著用循序教學加入設定、服務與資料存取。</p><Link to={`${docsPath}getting-started/quickstart/`}>開始建立第一個應用</Link></div>
        <CodeBlock language="bash" title="你的應用專案">{'cd hello-pangolin\ngo mod tidy\ngo run .\n# Hello, panGOlin!'}</CodeBlock>
      </section>
      <section className="container pg-status"><div><strong>依你的需求選擇功能。</strong><p>從模組與設定開始，逐步使用交易、登入、多租戶與事件。每個指南都說明前置需求、實際用法與目前版本的限制。</p></div><Link className="button button--secondary" to={`${docsPath}reference/compatibility/`}>查看功能相容性</Link></section>
    </main>
  </Layout>;
}

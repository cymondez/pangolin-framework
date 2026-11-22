import React from 'react';
import OriginalLinkItem from '@theme-original/Footer/LinkItem';
import {useActiveDocContext} from '@docusaurus/plugin-content-docs/client';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';

export default function VersionedFooterLink({item}) {
  const {activeVersion} = useActiveDocContext('default');
  const {siteConfig} = useDocusaurusContext();
  const prefix = siteConfig.customFields.docsPath;
  const to = activeVersion && item.to?.startsWith(prefix)
    ? `${activeVersion.path.replace(/\/$/, '')}/${item.to.slice(prefix.length)}`
    : item.to;
  return <OriginalLinkItem item={{...item, to}}/>;
}

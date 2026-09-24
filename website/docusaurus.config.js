// @ts-check
// The site describes atrium as of one release. It is rebuilt at release time, not per feature, so the version
// below moves with the tag and nothing else here should need to.

import {themes as prismThemes} from 'prism-react-renderer';

const release = '0.0.1';

// Where the site is served. GitHub Pages for dovholuknf/atrium by default. scripts/build-docs.ps1 passes the values
// the Pages action reports, so a fork publishes under its own address without editing this file.
const url = process.env.ATRIUM_DOCS_URL || 'https://dovholuknf.github.io';
const baseUrl = process.env.ATRIUM_DOCS_BASE_URL || '/atrium/';

/** @type {import('@docusaurus/types').Config} */
const config = {
  title: 'atrium',
  tagline: 'The single open hall every agent passes through.',
  favicon: 'img/favicon.svg',

  url,
  baseUrl,
  organizationName: 'dovholuknf',
  projectName: 'atrium',
  trailingSlash: false,

  onBrokenLinks: 'throw',
  onBrokenAnchors: 'throw',
  markdown: {
    hooks: {onBrokenMarkdownLinks: 'throw'},
  },

  customFields: {release},

  i18n: {defaultLocale: 'en', locales: ['en']},

  presets: [
    [
      'classic',
      /** @type {import('@docusaurus/preset-classic').Options} */
      ({
        docs: {
          sidebarPath: './sidebars.js',
          editUrl: 'https://github.com/dovholuknf/atrium/tree/main/website/',
        },
        blog: false,
        theme: {customCss: './src/css/custom.css'},
      }),
    ],
  ],

  themeConfig:
    /** @type {import('@docusaurus/preset-classic').ThemeConfig} */
    ({
      image: 'img/social-card.png',
      colorMode: {defaultMode: 'dark', respectPrefersColorScheme: true},
      navbar: {
        title: 'atrium',
        logo: {alt: 'The atrium A', src: 'img/logo.svg'},
        items: [
          {type: 'docSidebar', sidebarId: 'docs', position: 'left', label: 'Docs'},
          {to: '/docs/story', label: 'Why it exists', position: 'left'},
          {to: '/docs/install', label: 'Install', position: 'left'},
          {type: 'html', position: 'right', value: `<span class="navbar-release">v${release}</span>`},
          {href: 'https://github.com/dovholuknf/atrium', label: 'GitHub', position: 'right'},
        ],
      },
      footer: {
        style: 'dark',
        links: [
          {
            title: 'Start',
            items: [
              {label: 'What atrium is', to: '/docs/intro'},
              {label: 'Install', to: '/docs/install'},
              {label: 'Quick start', to: '/docs/quick-start'},
            ],
          },
          {
            title: 'Use it',
            items: [
              {label: 'The board', to: '/docs/board'},
              {label: 'Permissions and auto mode', to: '/docs/permissions'},
              {label: 'Supervised terminals', to: '/docs/terminals'},
            ],
          },
          {
            title: 'Project',
            items: [
              {label: 'Why it exists', to: '/docs/story'},
              {label: 'GitHub', href: 'https://github.com/dovholuknf/atrium'},
              {label: 'Apache 2.0', href: 'https://github.com/dovholuknf/atrium/blob/main/LICENSE'},
            ],
          },
        ],
        copyright: `atrium ${release}. Self-hosted, for one operator. No accounts, no hosted service.`,
      },
      prism: {
        theme: prismThemes.github,
        darkTheme: prismThemes.dracula,
        additionalLanguages: ['powershell', 'bash', 'json'],
      },
    }),
};

export default config;

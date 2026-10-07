// @ts-check

/** @type {import('@docusaurus/plugin-content-docs').SidebarsConfig} */
const sidebars = {
  docs: [
    {
      type: 'category',
      label: 'Start here',
      collapsed: false,
      items: ['intro', 'install', 'accounts', 'quick-start', 'how-it-works', 'modes', 'agents', 'differs', 'story'],
    },
    {
      type: 'category',
      label: 'Using atrium',
      collapsed: false,
      items: ['board', 'cards', 'permissions', 'terminals', 'messages', 'files', 'history', 'patterns'],
    },
    {
      type: 'category',
      label: 'Beyond one machine',
      collapsed: false,
      items: ['rooms', 'overlays'],
    },
    {
      type: 'category',
      label: 'Setting it up',
      collapsed: false,
      items: ['runners', 'intake', 'settings'],
    },
    {
      type: 'category',
      label: 'Reference',
      collapsed: false,
      items: ['cli', 'control-mcp', 'hooks'],
    },
  ],
};

export default sidebars;

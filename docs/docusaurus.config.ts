import { themes as prismThemes } from "prism-react-renderer";
import type { Config } from "@docusaurus/types";
import type * as Preset from "@docusaurus/preset-classic";

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

const config: Config = {
  title: "Muti Metroo",
  tagline: "Secure mesh networking for distributed infrastructure",
  favicon: "img/favicon.ico",

  headTags: [
    {
      tagName: "link",
      attributes: {
        rel: "icon",
        type: "image/png",
        sizes: "96x96",
        href: "/img/favicon-96x96.png",
      },
    },
    {
      tagName: "link",
      attributes: {
        rel: "icon",
        type: "image/svg+xml",
        href: "/img/favicon.svg",
      },
    },
    {
      tagName: "link",
      attributes: {
        rel: "apple-touch-icon",
        sizes: "180x180",
        href: "/img/apple-touch-icon.png",
      },
    },
    {
      tagName: "link",
      attributes: {
        rel: "manifest",
        href: "/site.webmanifest",
      },
    },
    // Analytics removed (no homepage)
  ],

  // Future flags, see https://docusaurus.io/docs/api/docusaurus-config#future
  future: {
    v4: true, // Improve compatibility with the upcoming Docusaurus v4
  },

  // Enable Mermaid diagrams
  markdown: {
    mermaid: true,
  },
  themes: [
    "@docusaurus/theme-mermaid",
    [
      "@easyops-cn/docusaurus-search-local",
      {
        hashed: true,
        docsRouteBasePath: "/",
        indexBlog: false,
      },
    ],
  ],

  // Set the production url of your site here
  url: "https://mutimetroo.com",
  // Set the /<baseUrl>/ pathname under which your site is served
  // For static site deployment, it is often '/<projectName>/'
  baseUrl: "/",

  // GitHub deployment config.
  organizationName: "postalsys", // GitHub organization
  projectName: "Muti-Metroo", // GitHub repo name

  onBrokenLinks: "warn",
  trailingSlash: true,

  // Even if you don't use internationalization, you can use this field to set
  // useful metadata like html lang. For example, if your site is Chinese, you
  // may want to replace "en" with "zh-Hans".
  i18n: {
    defaultLocale: "en",
    locales: ["en"],
  },

  presets: [
    [
      "classic",
      {
        docs: {
          sidebarPath: "./sidebars.ts",
          routeBasePath: "/",
        },
        blog: false,
        theme: {
          customCss: "./src/css/custom.css",
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    // Project social card
    image: "img/logo.png",
    colorMode: {
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: "Muti Metroo",
      logo: {
        alt: "Muti Metroo Logo",
        src: "img/logo.png",
      },
      items: [
        {
          type: "docSidebar",
          sidebarId: "docsSidebar",
          position: "left",
          label: "Documentation",
        },
        {
          to: "/download",
          label: "Download",
          position: "left",
        },
        {
          to: "/terms-of-service",
          label: "Terms",
          position: "right",
        },
        {
          href: "https://github.com/postalsys/Muti-Metroo",
          label: "Source",
          position: "right",
        },
        {
          type: "search",
          position: "right",
        },
      ],
    },
    footer: {
      style: "dark",
      links: [
        {
          title: "Documentation",
          items: [
            {
              label: "Getting Started",
              to: "/getting-started/overview",
            },
            {
              label: "Configuration",
              to: "/configuration/overview",
            },
            {
              label: "CLI Reference",
              to: "/cli/overview",
            },
            {
              label: "HTTP API",
              to: "/api/overview",
            },
          ],
        },
        {
          title: "Features",
          items: [
            {
              label: "SOCKS5 Proxy",
              to: "/features/socks5-proxy",
            },
            {
              label: "Exit Routing",
              to: "/features/exit-routing",
            },
            {
              label: "File Transfer",
              to: "/features/file-transfer",
            },
            {
              label: "Management Commands",
              to: "/features/shell",
            },
          ],
        },
        {
          title: "Resources",
          items: [
            {
              label: "Download",
              to: "/download",
            },
            {
              label: "Source Code",
              href: "https://github.com/postalsys/Muti-Metroo",
            },
            {
              label: "Troubleshooting",
              to: "/troubleshooting/common-issues",
            },
            {
              label: "Terms of Service",
              to: "/terms-of-service",
            },
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Postal Systems OÜ. All rights reserved.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: [
        "bash",
        "yaml",
        "json",
        "go",
        "nginx",
        "apacheconf",
      ],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;

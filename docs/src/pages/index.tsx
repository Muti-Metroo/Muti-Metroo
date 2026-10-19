import type {ReactNode} from 'react';
import clsx from 'clsx';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import HomepageFeatures from '@site/src/components/HomepageFeatures';
import NetworkDiagram from '@site/src/components/NetworkDiagram';
import Heading from '@theme/Heading';

import styles from './index.module.css';

function HomepageHeader() {
  const {siteConfig} = useDocusaurusContext();
  return (
    <header className={clsx('hero hero--primary', styles.heroBanner)}>
      <div className="container">
        <Heading as="h1" className="hero__title">
          {siteConfig.title}
        </Heading>
        <p className="hero__subtitle">{siteConfig.tagline}</p>
        <div className={styles.buttons}>
          <Link
            className="button button--secondary button--lg"
            to="/getting-started/quick-start">
            Get Started
          </Link>
          <Link
            className="button button--outline button--secondary button--lg"
            style={{marginLeft: '1rem'}}
            to="/intro">
            Learn More
          </Link>
        </div>
      </div>
    </header>
  );
}

function UseCases(): ReactNode {
  return (
    <section className={styles.useCases}>
      <div className="container">
        <div className="row">
          <div className="col col--12">
            <Heading as="h2" className="text--center margin-bottom--lg">
              Use Cases
            </Heading>
          </div>
        </div>
        <div className="row">
          <div className="col col--3">
            <div className={styles.useCase}>
              <Heading as="h3">Secure Remote Access</Heading>
              <p>
                Reach internal resources from your workstation through the
                Mutiauk TUN interface, without traditional VPN infrastructure.
                Stream payloads are encrypted end to end between ingress and
                exit agents.
              </p>
            </div>
          </div>
          <div className="col col--3">
            <div className={styles.useCase}>
              <Heading as="h3">Multi-Site Connectivity</Heading>
              <p>
                Connect data centers, offices, and cloud regions through one
                mesh. Exit agents advertise their networks and the routes
                propagate to every agent.
              </p>
            </div>
          </div>
          <div className="col col--3">
            <div className={styles.useCase}>
              <Heading as="h3">NAT and Network Traversal</Heading>
              <p>
                Connect across network boundaries using HTTP/2 or WebSocket
                transports compatible with corporate proxy infrastructure.
              </p>
            </div>
          </div>
          <div className="col col--3">
            <div className={styles.useCase}>
              <Heading as="h3">IoT and Edge Networking</Heading>
              <p>
                Connect distributed IoT devices and edge nodes through a
                lightweight mesh. A single binary with no dependencies runs on
                anything from a Raspberry Pi to a cloud VM.
              </p>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}

function QuickExample(): ReactNode {
  return (
    <section className={styles.example}>
      <div className="container">
        <div className="row">
          <div className="col col--12">
            <Heading as="h2" className="text--center margin-bottom--lg">
              Quick Example
            </Heading>
          </div>
        </div>
        <div className="row">
          <div className="col col--6">
            <Heading as="h4">1. Run agents on the mesh nodes</Heading>
            <pre className={styles.codeBlock}>
{`# Interactive setup wizard
muti-metroo setup

# Run with config
muti-metroo run -c config.yaml`}
            </pre>
          </div>
          <div className="col col--6">
            <Heading as="h4">2. Connect your workstation with Mutiauk</Heading>
            <pre className={styles.codeBlock}>
{`# Point Mutiauk at an ingress agent, receive the mesh's routes
sudo mutiauk setup
sudo mutiauk daemon start

# Ordinary tools now reach mesh networks
ssh admin@10.20.0.5`}
            </pre>
          </div>
        </div>
        <div className="row margin-top--lg">
          <div className="col col--12 text--center">
            <Link
              className="button button--primary button--lg"
              to="/getting-started/first-mesh">
              Build Your First Mesh
            </Link>
          </div>
        </div>
      </div>
    </section>
  );
}

export default function Home(): ReactNode {
  const {siteConfig} = useDocusaurusContext();
  return (
    <Layout
      title="Self-Hosted Mesh Tunneling"
      description="Secure mesh networking for distributed infrastructure. Run agents on your mesh nodes and reach their networks from your workstation through the Mutiauk TUN interface.">
      <HomepageHeader />
      <main>
        <NetworkDiagram />
        <HomepageFeatures />
        <UseCases />
        <QuickExample />
      </main>
    </Layout>
  );
}

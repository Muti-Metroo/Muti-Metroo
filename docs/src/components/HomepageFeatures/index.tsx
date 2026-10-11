import type {ReactNode} from 'react';
import clsx from 'clsx';
import Link from '@docusaurus/Link';
import Heading from '@theme/Heading';
import styles from './styles.module.css';

type FeatureItem = {
  title: string;
  image: string;
  imageAlt: string;
  description: ReactNode;
  link: string;
};

const FeatureList: FeatureItem[] = [
  {
    title: 'Network Traversal',
    image: '/img/mole-wiring.png',
    imageAlt: 'Mole connecting wires',
    link: '/concepts/transports',
    description: (
      <>
        Connect across network boundaries using HTTP/2 or WebSocket transports.
        Compatible with corporate proxy infrastructure.
      </>
    ),
  },
  {
    title: 'Single Binary, No Privileges',
    image: '/img/mole-drilling.png',
    imageAlt: 'Mole drilling through layers',
    link: '/concepts/architecture',
    description: (
      <>
        One binary, no root required, no kernel modules. Runs on Linux, macOS,
        and Windows, in containers, VMs, cloud instances, or bare metal.
      </>
    ),
  },
  {
    title: 'Multi-Hop Relay Chains',
    image: '/img/mole-plumbing.png',
    imageAlt: 'Mole connecting pipes',
    link: '/concepts/routing',
    description: (
      <>
        Build relay chains through multiple network segments. Routes propagate
        through the mesh, so traffic reaches the exit across any number of hops.
      </>
    ),
  },
  {
    title: 'SOCKS5 and TUN Interface',
    image: '/img/mole-escalator.png',
    imageAlt: 'Mole on escalator',
    link: '/features/socks5-proxy',
    description: (
      <>
        SOCKS proxy for application-level integration, or a TUN interface via
        the Mutiauk companion tool for transparent routing without per-app
        configuration.
      </>
    ),
  },
  {
    title: 'Remote Administration',
    image: '/img/mole-presenting.png',
    imageAlt: 'Mole presenting',
    link: '/features/shell',
    description: (
      <>
        Administer agents across the mesh with authenticated command execution
        and file transfer. Built-in security controls including command whitelisting.
      </>
    ),
  },
  {
    title: 'End-to-End Encrypted',
    image: '/img/mole-inspecting.png',
    imageAlt: 'Mole inspecting with magnifying glass',
    link: '/security/e2e-encryption',
    description: (
      <>
        Stream payloads are encrypted between ingress and exit with X25519 and
        ChaCha20-Poly1305, so transit agents relay data they do not decrypt.
      </>
    ),
  },
];

function Feature({title, image, imageAlt, description, link}: FeatureItem) {
  return (
    <div className={clsx('col col--4', styles.featureCol)}>
      <Link to={link} className={styles.featureLink}>
        <div className={styles.featureCard}>
          <div className="text--center padding-vert--md">
            <img src={image} alt={imageAlt} className={styles.featureImage} />
          </div>
          <div className="text--center padding-horiz--md">
            <Heading as="h3">{title}</Heading>
            <p>{description}</p>
          </div>
        </div>
      </Link>
    </div>
  );
}

export default function HomepageFeatures(): ReactNode {
  return (
    <section className={styles.features}>
      <div className="container">
        <div className="row">
          {FeatureList.map((props, idx) => (
            <Feature key={idx} {...props} />
          ))}
        </div>
      </div>
    </section>
  );
}

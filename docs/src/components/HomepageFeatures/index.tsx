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
        Deploy anywhere in seconds. One binary, no root required, no kernel
        modules. Runs on containers, VMs, cloud instances, or bare metal.
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
        Build relay chains through multiple network segments. Traffic
        automatically finds its way through any mesh topology to reach the exit.
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
        SOCKS5 proxy for application-level integration, or TUN interface via{' '}
        <Link to="/mutiauk">Mutiauk</Link> for transparent routing. Route any
        traffic through the mesh without per-app configuration.
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
        Transit nodes relay traffic they cannot decrypt. Built on proven
        cryptography (X25519 + ChaCha20-Poly1305) with zero trust by design.
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

import type {ReactNode} from 'react';
import Mermaid from '@theme/Mermaid';
import styles from './styles.module.css';

const diagramDefinition = `
flowchart LR
    subgraph client["Client Application"]
        C[("10.1.2.3")]
    end

    subgraph tun["Mutiauk TUN"]
        T["Transparent<br/>Interception"]
    end

    subgraph ingress["Ingress Agent"]
        S["SOCKS5 Proxy<br/>+ Route Lookup"]
    end

    subgraph mesh["Mesh Network"]
        TR1["Transit Agent"]
        TR2["Transit Agent"]
    end

    subgraph exit["Exit Agent"]
        E["TCP Connection<br/>to Target"]
    end

    subgraph target["Target"]
        D[("example.com")]
    end

    C -->|"Traffic"| T
    T -->|"Forward"| S
    S -->|"Encrypted<br/>Stream"| TR1
    TR1 -->|"Relay"| TR2
    TR2 -->|"Relay"| E
    E -->|"Connect"| D

    classDef clientStyle fill:#4a90d9,stroke:#2d5986,color:#fff
    classDef tunStyle fill:#9b59b6,stroke:#6c3483,color:#fff
    classDef ingressStyle fill:#27ae60,stroke:#1e8449,color:#fff
    classDef transitStyle fill:#f39c12,stroke:#b9770e,color:#fff
    classDef exitStyle fill:#e74c3c,stroke:#a93226,color:#fff
    classDef targetStyle fill:#34495e,stroke:#1c2833,color:#fff

    class C clientStyle
    class T tunStyle
    class S ingressStyle
    class TR1,TR2 transitStyle
    class E exitStyle
    class D targetStyle
`;

export default function NetworkDiagram(): ReactNode {
  return (
    <section className={styles.diagramSection}>
      <div className="container">
        <div className={styles.mermaidWrapper}>
          <Mermaid value={diagramDefinition} />
        </div>
      </div>
    </section>
  );
}

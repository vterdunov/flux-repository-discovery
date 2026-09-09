# Gruvbox Sand styling comparison

The original palette 7 followed by three CSS treatments of the same diagram. Colors, content, and layout are shared so the differences in depth and corners are easy to compare.

### 7. [Gruvbox Sand](https://github.com/morhetz/gruvbox)

Original: deeper sand, faded olive, and soft terracotta.

```mermaid
---
config:
  theme: base
  htmlLabels: false
  themeVariables:
    fontFamily: "system-ui, sans-serif"
    fontSize: "14px"
    primaryColor: "#d2b98b"
    primaryTextColor: "#3c3836"
    primaryBorderColor: "#8c755e"
    lineColor: "#8c755e"
    textColor: "#3c3836"
    clusterBkg: "#ddc9a4"
    clusterBorder: "#8c755e"
    titleColor: "#3c3836"
  flowchart:
    nodeSpacing: 16
    rankSpacing: 50
    padding: 20
    diagramPadding: 48
    subGraphTitleMargin:
      top: 6
      bottom: 8
---
flowchart TB
    github["GitHub repositories"]

    subgraph discovery["flux-repository-discovery"]
        direction LR
        scan["Periodic scan"] --> filters["Named filters<br/>include / exclude"]
        filters --> inputs["JSON inputs<br/>GET /inputs/{filter}"]
    end

    subgraph flux["Flux Operator"]
        direction LR
        provider["ExternalService provider"] --> resources["ResourceSet<br/>Kubernetes resources"]
    end

    github --> discovery --> flux

    classDef source fill:#e0c89b,stroke:#8c755e,color:#3c3836
    classDef output fill:#c2c18d,stroke:#8c755e,color:#3c3836
    classDef consumer fill:#cca98d,stroke:#8c755e,color:#3c3836
    class github source
    class inputs output
    class provider,resources consumer
```

### 7A. Soft shadows

Visible warm shadows and gently rounded nodes.

```mermaid
---
config:
  theme: base
  htmlLabels: false
  themeCSS: |
    .node rect { rx: 6px; ry: 6px; filter: drop-shadow(0px 2px 1px #3c383666) drop-shadow(0px 6px 5px #3c38364d); }
    .cluster rect { rx: 10px; ry: 10px; }
  themeVariables:
    fontFamily: "system-ui, sans-serif"
    fontSize: "14px"
    primaryColor: "#d2b98b"
    primaryTextColor: "#3c3836"
    primaryBorderColor: "#8c755e"
    lineColor: "#8c755e"
    textColor: "#3c3836"
    clusterBkg: "#ddc9a4"
    clusterBorder: "#8c755e"
    titleColor: "#3c3836"
  flowchart:
    nodeSpacing: 16
    rankSpacing: 50
    padding: 20
    diagramPadding: 48
    subGraphTitleMargin:
      top: 6
      bottom: 8
---
flowchart TB
    github["GitHub repositories"]

    subgraph discovery["flux-repository-discovery"]
        direction LR
        scan["Periodic scan"] --> filters["Named filters<br/>include / exclude"]
        filters --> inputs["JSON inputs<br/>GET /inputs/{filter}"]
    end

    subgraph flux["Flux Operator"]
        direction LR
        provider["ExternalService provider"] --> resources["ResourceSet<br/>Kubernetes resources"]
    end

    github --> discovery --> flux

    classDef source fill:#e0c89b,stroke:#8c755e,color:#3c3836
    classDef output fill:#c2c18d,stroke:#8c755e,color:#3c3836
    classDef consumer fill:#cca98d,stroke:#8c755e,color:#3c3836
    class github source
    class inputs output
    class provider,resources consumer
```

### 7B. Raised cards

Raised nodes with a firm contact shadow and a broader shadow under each group.

```mermaid
---
config:
  theme: base
  htmlLabels: false
  themeCSS: |
    .node rect { rx: 8px; ry: 8px; stroke-width: 1px; filter: drop-shadow(0px 3px 1px #3c383680) drop-shadow(0px 9px 7px #3c383659); }
    .cluster rect { rx: 12px; ry: 12px; filter: drop-shadow(0px 4px 2px #28282866) drop-shadow(0px 9px 9px #28282859); }
  themeVariables:
    fontFamily: "system-ui, sans-serif"
    fontSize: "14px"
    primaryColor: "#d2b98b"
    primaryTextColor: "#3c3836"
    primaryBorderColor: "#8c755e"
    lineColor: "#8c755e"
    textColor: "#3c3836"
    clusterBkg: "#ddc9a4"
    clusterBorder: "#8c755e"
    titleColor: "#3c3836"
  flowchart:
    nodeSpacing: 16
    rankSpacing: 50
    padding: 20
    diagramPadding: 48
    subGraphTitleMargin:
      top: 6
      bottom: 8
---
flowchart TB
    github["GitHub repositories"]

    subgraph discovery["flux-repository-discovery"]
        direction LR
        scan["Periodic scan"] --> filters["Named filters<br/>include / exclude"]
        filters --> inputs["JSON inputs<br/>GET /inputs/{filter}"]
    end

    subgraph flux["Flux Operator"]
        direction LR
        provider["ExternalService provider"] --> resources["ResourceSet<br/>Kubernetes resources"]
    end

    github --> discovery --> flux

    classDef source fill:#e0c89b,stroke:#8c755e,color:#3c3836
    classDef output fill:#c2c18d,stroke:#8c755e,color:#3c3836
    classDef consumer fill:#cca98d,stroke:#8c755e,color:#3c3836
    class github source
    class inputs output
    class provider,resources consumer
```

### 7C. Soft surfaces

Rounder corners and borderless surfaces defined by broad, layered shadows.

```mermaid
---
config:
  theme: base
  htmlLabels: false
  themeCSS: |
    .node rect { rx: 14px; ry: 14px; stroke-width: 0px; filter: drop-shadow(0px 3px 2px #3c383666) drop-shadow(0px 10px 8px #3c383659); }
    .cluster rect { rx: 18px; ry: 18px; stroke-width: 0px; filter: drop-shadow(0px 5px 2px #28282859) drop-shadow(0px 12px 10px #28282859); }
  themeVariables:
    fontFamily: "system-ui, sans-serif"
    fontSize: "14px"
    primaryColor: "#d2b98b"
    primaryTextColor: "#3c3836"
    primaryBorderColor: "#8c755e"
    lineColor: "#8c755e"
    textColor: "#3c3836"
    clusterBkg: "#ddc9a4"
    clusterBorder: "#8c755e"
    titleColor: "#3c3836"
  flowchart:
    nodeSpacing: 16
    rankSpacing: 50
    padding: 20
    diagramPadding: 48
    subGraphTitleMargin:
      top: 6
      bottom: 8
---
flowchart TB
    github["GitHub repositories"]

    subgraph discovery["flux-repository-discovery"]
        direction LR
        scan["Periodic scan"] --> filters["Named filters<br/>include / exclude"]
        filters --> inputs["JSON inputs<br/>GET /inputs/{filter}"]
    end

    subgraph flux["Flux Operator"]
        direction LR
        provider["ExternalService provider"] --> resources["ResourceSet<br/>Kubernetes resources"]
    end

    github --> discovery --> flux

    classDef source fill:#e0c89b,stroke:#8c755e,color:#3c3836
    classDef output fill:#c2c18d,stroke:#8c755e,color:#3c3836
    classDef consumer fill:#cca98d,stroke:#8c755e,color:#3c3836
    class github source
    class inputs output
    class provider,resources consumer
```

[Back to the project overview](../README.md#how-it-works).

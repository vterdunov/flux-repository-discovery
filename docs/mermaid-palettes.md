# Mermaid palette comparison

Twelve pastel adaptations of familiar editor palettes, each shown at full width. Each diagram uses the same discovery flow and layout; only the colors change. These are custom Mermaid palettes inspired by the linked themes.

### 1. [Gruvbox Soft](https://github.com/morhetz/gruvbox)

Warm sand, sage, and muted aqua.

```mermaid
---
config:
  theme: base
  themeVariables:
    fontFamily: "system-ui, sans-serif"
    fontSize: "14px"
    primaryColor: "#d5c4a1"
    primaryTextColor: "#3c3836"
    primaryBorderColor: "#928374"
    lineColor: "#928374"
    textColor: "#3c3836"
    clusterBkg: "#e4d7b9"
    clusterBorder: "#928374"
    titleColor: "#3c3836"
  flowchart:
    nodeSpacing: 16
    rankSpacing: 50
    padding: 12
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

    classDef source fill:#ebdbb2,stroke:#928374,color:#3c3836
    classDef output fill:#b7c7a4,stroke:#928374,color:#3c3836
    classDef consumer fill:#aac2b4,stroke:#928374,color:#3c3836
    class github source
    class inputs output
    class provider,resources consumer
```

### 2. [Tokyo Night](https://github.com/folke/tokyonight.nvim)

Cool blue, lavender, and mist.

```mermaid
---
config:
  theme: base
  themeVariables:
    fontFamily: "system-ui, sans-serif"
    fontSize: "14px"
    primaryColor: "#b9c8e1"
    primaryTextColor: "#343b58"
    primaryBorderColor: "#7c88ab"
    lineColor: "#7c88ab"
    textColor: "#343b58"
    clusterBkg: "#d5d9e5"
    clusterBorder: "#7c88ab"
    titleColor: "#343b58"
  flowchart:
    nodeSpacing: 16
    rankSpacing: 50
    padding: 12
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

    classDef source fill:#ccd0df,stroke:#7c88ab,color:#343b58
    classDef output fill:#c6badd,stroke:#7c88ab,color:#343b58
    classDef consumer fill:#b2ccd2,stroke:#7c88ab,color:#343b58
    class github source
    class inputs output
    class provider,resources consumer
```

### 3. [Catppuccin Latte](https://catppuccin.com/palette/)

Muted lilac, rosewater, and peach.

```mermaid
---
config:
  theme: base
  themeVariables:
    fontFamily: "system-ui, sans-serif"
    fontSize: "14px"
    primaryColor: "#d8c4df"
    primaryTextColor: "#4c4f69"
    primaryBorderColor: "#8c8fa1"
    lineColor: "#8c8fa1"
    textColor: "#4c4f69"
    clusterBkg: "#dce0e8"
    clusterBorder: "#8c8fa1"
    titleColor: "#4c4f69"
  flowchart:
    nodeSpacing: 16
    rankSpacing: 50
    padding: 12
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

    classDef source fill:#ccd0da,stroke:#8c8fa1,color:#4c4f69
    classDef output fill:#ddc4bd,stroke:#8c8fa1,color:#4c4f69
    classDef consumer fill:#dccaaf,stroke:#8c8fa1,color:#4c4f69
    class github source
    class inputs output
    class provider,resources consumer
```

### 4. [Nord](https://www.nordtheme.com/docs/colors-and-palettes/)

Frost blue, pale teal, and cool grey.

```mermaid
---
config:
  theme: base
  themeVariables:
    fontFamily: "system-ui, sans-serif"
    fontSize: "14px"
    primaryColor: "#b8cedb"
    primaryTextColor: "#2e3440"
    primaryBorderColor: "#6d8296"
    lineColor: "#6d8296"
    textColor: "#2e3440"
    clusterBkg: "#d8dee9"
    clusterBorder: "#6d8296"
    titleColor: "#2e3440"
  flowchart:
    nodeSpacing: 16
    rankSpacing: 50
    padding: 12
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

    classDef source fill:#c9d2df,stroke:#6d8296,color:#2e3440
    classDef output fill:#afd0cf,stroke:#6d8296,color:#2e3440
    classDef consumer fill:#b9c9ba,stroke:#6d8296,color:#2e3440
    class github source
    class inputs output
    class provider,resources consumer
```

### 5. [Solarized Light](https://ethanschoonover.com/solarized/)

Sand, faded cyan, and soft yellow.

```mermaid
---
config:
  theme: base
  themeVariables:
    fontFamily: "system-ui, sans-serif"
    fontSize: "14px"
    primaryColor: "#d2c9a5"
    primaryTextColor: "#41565d"
    primaryBorderColor: "#839496"
    lineColor: "#839496"
    textColor: "#41565d"
    clusterBkg: "#e5decb"
    clusterBorder: "#839496"
    titleColor: "#41565d"
  flowchart:
    nodeSpacing: 16
    rankSpacing: 50
    padding: 12
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

    classDef source fill:#d9d1ba,stroke:#839496,color:#41565d
    classDef output fill:#b7d0c7,stroke:#839496,color:#41565d
    classDef consumer fill:#d9c998,stroke:#839496,color:#41565d
    class github source
    class inputs output
    class provider,resources consumer
```

### 6. [Rosé Pine Dawn](https://rosepinetheme.com/palette/)

Dusty rose, muted iris, and warm grey.

```mermaid
---
config:
  theme: base
  themeVariables:
    fontFamily: "system-ui, sans-serif"
    fontSize: "14px"
    primaryColor: "#d8c4ca"
    primaryTextColor: "#514d6e"
    primaryBorderColor: "#9893a5"
    lineColor: "#9893a5"
    textColor: "#514d6e"
    clusterBkg: "#e8ded8"
    clusterBorder: "#9893a5"
    titleColor: "#514d6e"
  flowchart:
    nodeSpacing: 16
    rankSpacing: 50
    padding: 12
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

    classDef source fill:#dcd1cc,stroke:#9893a5,color:#514d6e
    classDef output fill:#c9c3dd,stroke:#9893a5,color:#514d6e
    classDef consumer fill:#b9ced1,stroke:#9893a5,color:#514d6e
    class github source
    class inputs output
    class provider,resources consumer
```

## More Gruvbox and Monokai variants

These variant names describe custom pastel adaptations. Compare them with the original six above.

### 7. [Gruvbox Sand](https://github.com/morhetz/gruvbox)

Deeper sand, faded olive, and soft terracotta.

```mermaid
---
config:
  theme: base
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
    padding: 12
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

### 8. [Monokai Soft](https://monokai.pro/contribute)

Muted lime, cyan, and pink on warm grey.

```mermaid
---
config:
  theme: base
  themeVariables:
    fontFamily: "system-ui, sans-serif"
    fontSize: "14px"
    primaryColor: "#bfcc98"
    primaryTextColor: "#3e3b3f"
    primaryBorderColor: "#8a8785"
    lineColor: "#8a8785"
    textColor: "#3e3b3f"
    clusterBkg: "#d3d0c4"
    clusterBorder: "#8a8785"
    titleColor: "#3e3b3f"
  flowchart:
    nodeSpacing: 16
    rankSpacing: 50
    padding: 12
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

    classDef source fill:#dfd3a4,stroke:#8a8785,color:#3e3b3f
    classDef output fill:#acccd2,stroke:#8a8785,color:#3e3b3f
    classDef consumer fill:#d6afc0,stroke:#8a8785,color:#3e3b3f
    class github source
    class inputs output
    class provider,resources consumer
```

### 9. [Gruvbox Sage](https://github.com/morhetz/gruvbox)

Sage, moss, and dusty aqua.

```mermaid
---
config:
  theme: base
  themeVariables:
    fontFamily: "system-ui, sans-serif"
    fontSize: "14px"
    primaryColor: "#b4c29b"
    primaryTextColor: "#3c3836"
    primaryBorderColor: "#79846a"
    lineColor: "#79846a"
    textColor: "#3c3836"
    clusterBkg: "#cad2b8"
    clusterBorder: "#79846a"
    titleColor: "#3c3836"
  flowchart:
    nodeSpacing: 16
    rankSpacing: 50
    padding: 12
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

    classDef source fill:#d5c4a1,stroke:#79846a,color:#3c3836
    classDef output fill:#a7c5b0,stroke:#79846a,color:#3c3836
    classDef consumer fill:#b5c7ba,stroke:#79846a,color:#3c3836
    class github source
    class inputs output
    class provider,resources consumer
```

### 10. [Monokai Aqua](https://monokai.pro/contribute)

Dusty cyan, faded lime, and lilac.

```mermaid
---
config:
  theme: base
  themeVariables:
    fontFamily: "system-ui, sans-serif"
    fontSize: "14px"
    primaryColor: "#a9c8ce"
    primaryTextColor: "#34383d"
    primaryBorderColor: "#7c8e94"
    lineColor: "#7c8e94"
    textColor: "#34383d"
    clusterBkg: "#c6d5d6"
    clusterBorder: "#7c8e94"
    titleColor: "#34383d"
  flowchart:
    nodeSpacing: 16
    rankSpacing: 50
    padding: 12
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

    classDef source fill:#c3c4d7,stroke:#7c8e94,color:#34383d
    classDef output fill:#c3ca99,stroke:#7c8e94,color:#34383d
    classDef consumer fill:#bdafd3,stroke:#7c8e94,color:#34383d
    class github source
    class inputs output
    class provider,resources consumer
```

### 11. [Gruvbox Clay](https://github.com/morhetz/gruvbox)

Clay, muted plum, and olive.

```mermaid
---
config:
  theme: base
  themeVariables:
    fontFamily: "system-ui, sans-serif"
    fontSize: "14px"
    primaryColor: "#cda68d"
    primaryTextColor: "#3c3836"
    primaryBorderColor: "#987a6b"
    lineColor: "#987a6b"
    textColor: "#3c3836"
    clusterBkg: "#dcc9b8"
    clusterBorder: "#987a6b"
    titleColor: "#3c3836"
  flowchart:
    nodeSpacing: 16
    rankSpacing: 50
    padding: 12
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

    classDef source fill:#d5c4a1,stroke:#987a6b,color:#3c3836
    classDef output fill:#c7b5be,stroke:#987a6b,color:#3c3836
    classDef consumer fill:#b3bfa2,stroke:#987a6b,color:#3c3836
    class github source
    class inputs output
    class provider,resources consumer
```

### 12. [Monokai Rose](https://monokai.pro/contribute)

Dusty pink, apricot, and soft violet.

```mermaid
---
config:
  theme: base
  themeVariables:
    fontFamily: "system-ui, sans-serif"
    fontSize: "14px"
    primaryColor: "#cfa6b7"
    primaryTextColor: "#3e3b3f"
    primaryBorderColor: "#91838f"
    lineColor: "#91838f"
    textColor: "#3e3b3f"
    clusterBkg: "#d8ccd2"
    clusterBorder: "#91838f"
    titleColor: "#3e3b3f"
  flowchart:
    nodeSpacing: 16
    rankSpacing: 50
    padding: 12
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

    classDef source fill:#d9bd9e,stroke:#91838f,color:#3e3b3f
    classDef output fill:#afa4cb,stroke:#91838f,color:#3e3b3f
    classDef consumer fill:#b8c598,stroke:#91838f,color:#3e3b3f
    class github source
    class inputs output
    class provider,resources consumer
```

[Back to the project overview](../README.md#how-it-works).

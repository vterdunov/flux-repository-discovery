# Mermaid palette comparison

Six pastel adaptations of familiar editor palettes, shown side by side. Each diagram uses the same discovery flow and layout; only the colors change. These are custom Mermaid palettes inspired by the linked themes.

<table width="100%">
<tr>
<td width="50%" valign="top">

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
        scan["Periodic scan"] --> filters["Named filters<br/>include / exclude"]
        filters --> inputs["JSON inputs<br/>GET /inputs/{filter}"]
    end

    subgraph flux["Flux Operator"]
        provider["ExternalService provider"] --> resources["ResourceSet<br/>Kubernetes resources"]
    end

    github --> scan
    inputs --> provider

    classDef source fill:#ebdbb2,stroke:#928374,color:#3c3836
    classDef output fill:#b7c7a4,stroke:#928374,color:#3c3836
    classDef consumer fill:#aac2b4,stroke:#928374,color:#3c3836
    class github source
    class inputs output
    class provider,resources consumer
```

</td>
<td width="50%" valign="top">

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
        scan["Periodic scan"] --> filters["Named filters<br/>include / exclude"]
        filters --> inputs["JSON inputs<br/>GET /inputs/{filter}"]
    end

    subgraph flux["Flux Operator"]
        provider["ExternalService provider"] --> resources["ResourceSet<br/>Kubernetes resources"]
    end

    github --> scan
    inputs --> provider

    classDef source fill:#ccd0df,stroke:#7c88ab,color:#343b58
    classDef output fill:#c6badd,stroke:#7c88ab,color:#343b58
    classDef consumer fill:#b2ccd2,stroke:#7c88ab,color:#343b58
    class github source
    class inputs output
    class provider,resources consumer
```

</td>
</tr>
<tr>
<td width="50%" valign="top">

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
        scan["Periodic scan"] --> filters["Named filters<br/>include / exclude"]
        filters --> inputs["JSON inputs<br/>GET /inputs/{filter}"]
    end

    subgraph flux["Flux Operator"]
        provider["ExternalService provider"] --> resources["ResourceSet<br/>Kubernetes resources"]
    end

    github --> scan
    inputs --> provider

    classDef source fill:#ccd0da,stroke:#8c8fa1,color:#4c4f69
    classDef output fill:#ddc4bd,stroke:#8c8fa1,color:#4c4f69
    classDef consumer fill:#dccaaf,stroke:#8c8fa1,color:#4c4f69
    class github source
    class inputs output
    class provider,resources consumer
```

</td>
<td width="50%" valign="top">

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
        scan["Periodic scan"] --> filters["Named filters<br/>include / exclude"]
        filters --> inputs["JSON inputs<br/>GET /inputs/{filter}"]
    end

    subgraph flux["Flux Operator"]
        provider["ExternalService provider"] --> resources["ResourceSet<br/>Kubernetes resources"]
    end

    github --> scan
    inputs --> provider

    classDef source fill:#c9d2df,stroke:#6d8296,color:#2e3440
    classDef output fill:#afd0cf,stroke:#6d8296,color:#2e3440
    classDef consumer fill:#b9c9ba,stroke:#6d8296,color:#2e3440
    class github source
    class inputs output
    class provider,resources consumer
```

</td>
</tr>
<tr>
<td width="50%" valign="top">

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
        scan["Periodic scan"] --> filters["Named filters<br/>include / exclude"]
        filters --> inputs["JSON inputs<br/>GET /inputs/{filter}"]
    end

    subgraph flux["Flux Operator"]
        provider["ExternalService provider"] --> resources["ResourceSet<br/>Kubernetes resources"]
    end

    github --> scan
    inputs --> provider

    classDef source fill:#d9d1ba,stroke:#839496,color:#41565d
    classDef output fill:#b7d0c7,stroke:#839496,color:#41565d
    classDef consumer fill:#d9c998,stroke:#839496,color:#41565d
    class github source
    class inputs output
    class provider,resources consumer
```

</td>
<td width="50%" valign="top">

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
        scan["Periodic scan"] --> filters["Named filters<br/>include / exclude"]
        filters --> inputs["JSON inputs<br/>GET /inputs/{filter}"]
    end

    subgraph flux["Flux Operator"]
        provider["ExternalService provider"] --> resources["ResourceSet<br/>Kubernetes resources"]
    end

    github --> scan
    inputs --> provider

    classDef source fill:#dcd1cc,stroke:#9893a5,color:#514d6e
    classDef output fill:#c9c3dd,stroke:#9893a5,color:#514d6e
    classDef consumer fill:#b9ced1,stroke:#9893a5,color:#514d6e
    class github source
    class inputs output
    class provider,resources consumer
```

</td>
</tr>
</table>

[Back to the project overview](../README.md#how-it-works).

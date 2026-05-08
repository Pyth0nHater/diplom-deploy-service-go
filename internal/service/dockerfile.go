package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	appTypeAuto      = "auto"
	appTypeStatic    = "static"
	appTypeReact     = "react"
	appTypeVue       = "vue"
	appTypeAngular   = "angular"
	appTypeNextJS    = "nextjs"
	defaultNodeMajor = "20"
)

type appProfile struct {
	Name         string
	InternalPort string
}

type packageJSON struct {
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Engines         map[string]string `json:"engines"`
}

type repoMetadata struct {
	Package *packageJSON
}

type nodeRuntime struct {
	Image   string
	Version string
	Source  string
}

const nextConfigHelperPath = ".deploy-service/next-base-path.cjs"

var versionPattern = regexp.MustCompile(`\d+(?:\.\d+){0,2}`)

func resolveAppProfile(workDir, requestedType string) (appProfile, string, error) {
	switch normalizeAppType(requestedType) {
	case "", appTypeAuto:
		profile, err := detectAppProfile(workDir)
		if err != nil {
			return appProfile{}, "", err
		}
		return profile, fmt.Sprintf("Detected %s app profile", profile.Name), nil
	case appTypeStatic, appTypeReact, appTypeVue, appTypeAngular:
		return staticAppProfile(), fmt.Sprintf("Using requested %s app profile", normalizeAppType(requestedType)), nil
	case appTypeNextJS:
		return nextAppProfile(), "Using requested nextjs app profile", nil
	default:
		return appProfile{}, "", fmt.Errorf("unsupported app_type %q (supported: auto, static, react, vue, angular, nextjs)", requestedType)
	}
}

func detectAppProfile(workDir string) (appProfile, error) {
	metadata, err := readRepoMetadata(workDir)
	if err != nil {
		return appProfile{}, err
	}
	if metadata.Package != nil {
		pkg := *metadata.Package
		switch {
		case isNextApp(pkg):
			return nextAppProfile(), nil
		case isAngularApp(pkg):
			return angularAppProfile(), nil
		case isVueApp(pkg):
			return vueAppProfile(), nil
		case isReactApp(pkg):
			return reactAppProfile(), nil
		}
	}

	return staticAppProfile(), nil
}

func readRepoMetadata(workDir string) (repoMetadata, error) {
	data, err := os.ReadFile(filepath.Join(workDir, "package.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return repoMetadata{}, nil
		}
		return repoMetadata{}, err
	}

	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return repoMetadata{}, err
	}
	return repoMetadata{Package: &pkg}, nil
}

func normalizeAppType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isNextApp(pkg packageJSON) bool {
	if hasPackage(pkg.Dependencies, "next") || hasPackage(pkg.DevDependencies, "next") {
		return true
	}
	for _, script := range pkg.Scripts {
		if strings.Contains(script, "next build") || strings.Contains(script, "next start") || strings.Contains(script, "next dev") {
			return true
		}
	}
	return false
}

func isAngularApp(pkg packageJSON) bool {
	return hasPackage(pkg.Dependencies, "@angular/core") || hasPackage(pkg.DevDependencies, "@angular/core")
}

func isVueApp(pkg packageJSON) bool {
	return hasPackage(pkg.Dependencies, "vue") || hasPackage(pkg.DevDependencies, "vue")
}

func isReactApp(pkg packageJSON) bool {
	return hasPackage(pkg.Dependencies, "react") || hasPackage(pkg.DevDependencies, "react") ||
		hasPackage(pkg.Dependencies, "react-dom") || hasPackage(pkg.DevDependencies, "react-dom")
}

func hasPackage(packages map[string]string, name string) bool {
	if packages == nil {
		return false
	}
	_, ok := packages[name]
	return ok
}

func staticAppProfile() appProfile {
	return appProfile{Name: "static", InternalPort: "80"}
}

func reactAppProfile() appProfile {
	return appProfile{Name: "react", InternalPort: "80"}
}

func vueAppProfile() appProfile {
	return appProfile{Name: "vue", InternalPort: "80"}
}

func angularAppProfile() appProfile {
	return appProfile{Name: "angular", InternalPort: "80"}
}

func nextAppProfile() appProfile {
	return appProfile{Name: "nextjs", InternalPort: "3000"}
}

func renderDockerfile(profile appProfile, nodeImage, basePath string) string {
	switch profile.Name {
	case appTypeNextJS:
		return nextAppDockerfile(nodeImage, basePath)
	default:
		return staticAppDockerfile(nodeImage, basePath)
	}
}

func staticAppDockerfile(nodeImage, basePath string) string {
	return fmt.Sprintf(strings.TrimSpace(`
FROM %s AS build
WORKDIR /app
ENV DEPLOY_BASE_PATH=%q
ENV PUBLIC_URL=%q
ENV BASE_URL=%q
COPY package*.json ./
RUN if [ -f package-lock.json ]; then npm ci --legacy-peer-deps; else npm install --legacy-peer-deps; fi
COPY . .
RUN npm run build
RUN if [ -n "$DEPLOY_BASE_PATH" ] && [ "$DEPLOY_BASE_PATH" != "/" ]; then \
      for dir in /app/dist /app/build /app/out; do \
        if [ -d "$dir" ]; then \
          find "$dir" -name '*.html' -exec sed -i \
            -e "s|href=\"/|href=\"$DEPLOY_BASE_PATH/|g" \
            -e "s|src=\"/|src=\"$DEPLOY_BASE_PATH/|g" \
            -e "s|content=\"/|content=\"$DEPLOY_BASE_PATH/|g" \
            -e "s|url(/|url($DEPLOY_BASE_PATH/|g" {} +; \
        fi; \
      done; \
    fi
RUN mkdir -p /opt/app-static \
 && if [ -d /app/dist ]; then cp -R /app/dist/. /opt/app-static/; \
 elif [ -d /app/build ]; then cp -R /app/build/. /opt/app-static/; \
 elif [ -d /app/out ]; then cp -R /app/out/. /opt/app-static/; \
 else echo "No static build output found. Expected one of: dist, build, out." >&2; exit 1; fi

FROM nginx:stable-alpine
COPY --from=build /opt/app-static/ /usr/share/nginx/html/
RUN echo 'server { listen 80; location / { root /usr/share/nginx/html; try_files $uri $uri/ /index.html; } }' > /etc/nginx/conf.d/default.conf
EXPOSE 80
`), nodeImage, basePath, basePath, basePath) + "\n"
}

func nextAppDockerfile(nodeImage, basePath string) string {
	return fmt.Sprintf(strings.TrimSpace(`
FROM %s AS build
WORKDIR /app
ENV DEPLOY_BASE_PATH=%q
ENV NEXT_PUBLIC_BASE_PATH=%q
COPY package*.json ./
RUN if [ -f package-lock.json ]; then npm ci --legacy-peer-deps; else npm install --legacy-peer-deps; fi
COPY . .
RUN node %s
RUN npm run build

FROM %s
WORKDIR /app
ENV NODE_ENV=production
ENV HOSTNAME=0.0.0.0
ENV PORT=3000
ENV DEPLOY_BASE_PATH=%q
ENV NEXT_PUBLIC_BASE_PATH=%q
COPY --from=build /app /app
EXPOSE 3000
CMD ["npm", "run", "start"]
`), nodeImage, basePath, basePath, nextConfigHelperPath, nodeImage, basePath, basePath) + "\n"
}

func resolveNodeRuntime(workDir string, params DeployParams) (nodeRuntime, string, error) {
	if value := strings.TrimSpace(params.NodeVersion); value != "" {
		runtime, err := nodeRuntimeFromValue(value, "requested node_version")
		if err != nil {
			return nodeRuntime{}, "", err
		}
		return runtime, fmt.Sprintf("Using %s from requested node_version", runtime.Image), nil
	}

	for _, candidate := range []struct {
		path   string
		source string
	}{
		{path: filepath.Join(workDir, ".nvmrc"), source: ".nvmrc"},
		{path: filepath.Join(workDir, ".node-version"), source: ".node-version"},
	} {
		runtime, ok, err := nodeRuntimeFromVersionFile(candidate.path, candidate.source)
		if err != nil {
			return nodeRuntime{}, "", err
		}
		if ok {
			return runtime, fmt.Sprintf("Using %s from %s", runtime.Image, runtime.Source), nil
		}
	}

	metadata, err := readRepoMetadata(workDir)
	if err != nil {
		return nodeRuntime{}, "", err
	}
	if metadata.Package != nil && metadata.Package.Engines != nil {
		if value := strings.TrimSpace(metadata.Package.Engines["node"]); value != "" {
			runtime, err := nodeRuntimeFromConstraint(value, "package.json engines.node")
			if err != nil {
				return nodeRuntime{}, "", err
			}
			return runtime, fmt.Sprintf("Using %s from %s=%q", runtime.Image, runtime.Source, value), nil
		}
	}

	defaultValue := envOrDefault("DEPLOY_DEFAULT_NODE_VERSION", defaultNodeMajor)
	runtime, err := nodeRuntimeFromValue(defaultValue, "DEPLOY_DEFAULT_NODE_VERSION")
	if err != nil {
		return nodeRuntime{}, "", err
	}
	return runtime, fmt.Sprintf("Using %s from %s", runtime.Image, runtime.Source), nil
}

func nodeRuntimeFromVersionFile(path, source string) (nodeRuntime, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nodeRuntime{}, false, nil
		}
		return nodeRuntime{}, false, err
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		runtime, err := nodeRuntimeFromValue(line, source)
		if err != nil {
			return nodeRuntime{}, false, err
		}
		return runtime, true, nil
	}

	return nodeRuntime{}, false, nil
}

func nodeRuntimeFromConstraint(constraint, source string) (nodeRuntime, error) {
	for _, clause := range strings.Split(constraint, "||") {
		if version := firstVersionCandidate(clause); version != "" {
			return nodeRuntimeFromValue(version, source)
		}
	}
	return nodeRuntime{}, fmt.Errorf("could not derive node version from %s=%q", source, constraint)
}

func nodeRuntimeFromValue(value, source string) (nodeRuntime, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "node:")
	value = strings.TrimSpace(strings.TrimPrefix(value, "v"))
	value = strings.TrimSuffix(value, "-alpine")

	switch value {
	case "", "current", "node", "stable", "latest", "lts", "lts/*", "lts/-1":
		value = envOrDefault("DEPLOY_DEFAULT_NODE_VERSION", defaultNodeMajor)
		value = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(value), "v"))
	}

	version := firstVersionCandidate(value)
	if version == "" {
		return nodeRuntime{}, fmt.Errorf("unsupported node version %q from %s", value, source)
	}

	return nodeRuntime{
		Image:   "node:" + version + "-alpine",
		Version: version,
		Source:  source,
	}, nil
}

func firstVersionCandidate(value string) string {
	match := versionPattern.FindString(strings.TrimSpace(value))
	return strings.TrimSpace(match)
}

func nextBasePathHelperScript() string {
	lines := []string{
		`const fs = require("fs");`,
		`const path = require("path");`,
		``,
		`const projectRoot = process.cwd();`,
		`const basePath = normalizeBasePath(process.env.DEPLOY_BASE_PATH || process.env.NEXT_PUBLIC_BASE_PATH || "");`,
		``,
		`if (!basePath || basePath === "/") {`,
		`  process.exit(0);`,
		`}`,
		``,
		`const packageJsonPath = path.join(projectRoot, "package.json");`,
		`const packageType = readPackageType(packageJsonPath);`,
		`const configFile = findNextConfig(projectRoot);`,
		``,
		`if (!configFile) {`,
		`  const wrapperPath = path.join(projectRoot, "next.config.mjs");`,
		`  fs.writeFileSync(wrapperPath, createEsmWrapper(null, basePath), "utf8");`,
		`  process.exit(0);`,
		`}`,
		``,
		`if (configFile.name.startsWith(".deploy-service.")) {`,
		`  process.exit(0);`,
		`}`,
		``,
		`const originalName = ".deploy-service.original." + configFile.name;`,
		`const originalPath = path.join(projectRoot, originalName);`,
		``,
		`if (!fs.existsSync(originalPath)) {`,
		`  fs.renameSync(configFile.path, originalPath);`,
		`}`,
		``,
		`const wrapperPath = configFile.path;`,
		`let wrapperContent;`,
		``,
		`switch (configFile.ext) {`,
		`  case ".mjs":`,
		`    wrapperContent = createEsmWrapper("./" + originalName, basePath);`,
		`    break;`,
		`  case ".cjs":`,
		`    wrapperContent = createCjsWrapper("./" + originalName, basePath);`,
		`    break;`,
		`  case ".ts":`,
		`    wrapperContent = createTsWrapper("./" + originalName, basePath);`,
		`    break;`,
		`  case ".js":`,
		`    if (packageType === "module") {`,
		`      wrapperContent = createEsmWrapper("./" + originalName, basePath);`,
		`    } else {`,
		`      wrapperContent = createCjsWrapper("./" + originalName, basePath);`,
		`    }`,
		`    break;`,
		`  default:`,
		`    throw new Error("Unsupported Next config extension: " + configFile.ext);`,
		`}`,
		``,
		`fs.writeFileSync(wrapperPath, wrapperContent, "utf8");`,
		``,
		`function normalizeBasePath(value) {`,
		`  const trimmed = String(value || "").trim();`,
		`  if (!trimmed) return "";`,
		`  if (trimmed === "/") return "/";`,
		`  return "/" + trimmed.replace(/^\/+|\/+$/g, "");`,
		`}`,
		``,
		`function readPackageType(packageJsonPath) {`,
		`  try {`,
		`    const pkg = JSON.parse(fs.readFileSync(packageJsonPath, "utf8"));`,
		`    return pkg.type === "module" ? "module" : "commonjs";`,
		`  } catch {`,
		`    return "commonjs";`,
		`  }`,
		`}`,
		``,
		`function findNextConfig(root) {`,
		`  const names = ["next.config.mjs", "next.config.cjs", "next.config.js", "next.config.ts"];`,
		`  for (const name of names) {`,
		`    const configPath = path.join(root, name);`,
		`    if (fs.existsSync(configPath)) {`,
		`      return { name, path: configPath, ext: path.extname(name) };`,
		`    }`,
		`  }`,
		`  return null;`,
		`}`,
		``,
		`function sharedWrapperBody(basePathLiteral) {`,
		`  return "const deployServiceBasePath = " + JSON.stringify(basePathLiteral) + ";\n\n" +`,
		`    "function mergeBasePath(config) {\n" +`,
		`    "  const nextConfig = config && typeof config === \"object\" ? config : {};\n" +`,
		`    "  return {\n" +`,
		`    "    ...nextConfig,\n" +`,
		`    "    basePath: deployServiceBasePath,\n" +`,
		`    "    assetPrefix: deployServiceBasePath === \"/\" ? undefined : deployServiceBasePath,\n" +`,
		`    "  };\n" +`,
		`    "}\n\n" +`,
		`    "function wrapConfig(config) {\n" +`,
		`    "  if (typeof config === \"function\") {\n" +`,
		`    "    return async (...args) => mergeBasePath(await config(...args));\n" +`,
		`    "  }\n" +`,
		`    "  return mergeBasePath(config);\n" +`,
		`    "}\n";`,
		`}`,
		``,
		`function createEsmWrapper(importPath, basePathLiteral) {`,
		`  const importBlock = importPath`,
		`    ? "import * as originalModule from " + JSON.stringify(importPath) + ";\n" + "const originalConfig = originalModule.default ?? originalModule;\n"`,
		`    : "const originalConfig = {};\n";`,
		``,
		`  return importBlock + sharedWrapperBody(basePathLiteral) + "\nexport default wrapConfig(originalConfig);\n";`,
		`}`,
		``,
		`function createCjsWrapper(importPath, basePathLiteral) {`,
		`  const importBlock = importPath`,
		`    ? "const originalModule = require(" + JSON.stringify(importPath) + ");\n" + "const originalConfig = originalModule.default ?? originalModule;\n"`,
		`    : "const originalConfig = {};\n";`,
		``,
		`  return importBlock + sharedWrapperBody(basePathLiteral) + "\nmodule.exports = wrapConfig(originalConfig);\n";`,
		`}`,
		``,
		`function createTsWrapper(importPath, basePathLiteral) {`,
		`  const strippedPath = importPath ? importPath.replace(/\.ts$/, "") : importPath;`,
		`  const importBlock = strippedPath`,
		`    ? "// @ts-nocheck\nimport * as originalModule from " + JSON.stringify(strippedPath) + ";\nconst originalConfig = originalModule.default ?? originalModule;\n"`,
		`    : "// @ts-nocheck\nconst originalConfig = {};\n";`,
		``,
		`  return importBlock + sharedWrapperBody(basePathLiteral) + "\nexport default wrapConfig(originalConfig);\n";`,
		`}`,
	}
	return strings.Join(lines, "\n") + "\n"
}

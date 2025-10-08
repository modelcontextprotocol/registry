#!/usr/bin/env node
/**
 * Convert seed data packages to canonical reference format
 *
 * This script:
 * - Converts OCI packages to use canonical single-line references
 * - Removes registryBaseUrl and version fields from OCI packages
 * - Preserves NPM, PyPI, NuGet packages unchanged (they already use canonical format)
 * - Updates MCPB packages to remove version field (embedded in identifier URL)
 */

const fs = require('fs');
const path = require('path');

const SEED_FILE = path.join(__dirname, '../data/seed.json');

function convertOCIPackage(pkg) {
  if (pkg.registryType !== 'oci') {
    return pkg;
  }

  const result = { ...pkg };

  // Get current values
  const registryBaseUrl = pkg.registryBaseUrl;
  const identifier = pkg.identifier;
  const version = pkg.version;

  // Skip if already in canonical format (has registry in identifier)
  if (!registryBaseUrl && (identifier.includes(':') || identifier.includes('@sha256:'))) {
    return result;
  }

  // Extract registry host from registryBaseUrl or default to docker.io
  let registryHost = 'docker.io';
  if (registryBaseUrl) {
    registryHost = registryBaseUrl.replace(/^https?:\/\//, '');
  }

  // Build canonical reference
  let canonicalRef;
  if (version) {
    canonicalRef = `${registryHost}/${identifier}:${version}`;
  } else {
    canonicalRef = `${registryHost}/${identifier}:latest`;
  }

  // Update identifier to canonical reference
  result.identifier = canonicalRef;

  // Remove registryBaseUrl field (no longer needed for OCI)
  delete result.registryBaseUrl;

  // Remove version field (now part of identifier for OCI)
  delete result.version;

  return result;
}

function convertMCPBPackage(pkg) {
  if (pkg.registryType !== 'mcpb') {
    return pkg;
  }

  const result = { ...pkg };

  // MCPB packages should not have a separate version field
  // The version is embedded in the URL identifier
  delete result.version;

  return result;
}

function convertPackage(pkg) {
  // Convert OCI packages
  if (pkg.registryType === 'oci') {
    return convertOCIPackage(pkg);
  }

  // Convert MCPB packages
  if (pkg.registryType === 'mcpb') {
    return convertMCPBPackage(pkg);
  }

  // NPM, PyPI, NuGet packages are already in correct format
  return pkg;
}

function convertServer(server) {
  if (!server.packages || !Array.isArray(server.packages)) {
    return server;
  }

  return {
    ...server,
    packages: server.packages.map(convertPackage)
  };
}

function main() {
  console.log('Converting seed data to canonical package format...');

  // Read seed file
  const seedData = JSON.parse(fs.readFileSync(SEED_FILE, 'utf8'));

  if (!Array.isArray(seedData)) {
    console.error('Error: seed.json must contain an array of servers');
    process.exit(1);
  }

  // Track conversions
  let ociConverted = 0;
  let mcpbConverted = 0;

  // Convert all servers
  const convertedData = seedData.map(server => {
    const converted = convertServer(server);

    // Count conversions
    if (server.packages) {
      server.packages.forEach((pkg, idx) => {
        if (pkg.registryType === 'oci' && pkg.version) {
          ociConverted++;
          console.log(`  Converted OCI: ${pkg.identifier}:${pkg.version} -> ${converted.packages[idx].identifier}`);
        }
        if (pkg.registryType === 'mcpb' && pkg.version) {
          mcpbConverted++;
        }
      });
    }

    return converted;
  });

  // Write back to file
  fs.writeFileSync(SEED_FILE, JSON.stringify(convertedData, null, 2) + '\n', 'utf8');

  console.log(`\nConversion complete!`);
  console.log(`  OCI packages converted: ${ociConverted}`);
  console.log(`  MCPB packages converted: ${mcpbConverted}`);
  console.log(`  Total servers: ${convertedData.length}`);
}

main();

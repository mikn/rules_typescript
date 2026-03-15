/** @type {import('next').NextConfig} */
const nextConfig = {
  // No transpilePackages needed: shared package sources are staged at their
  // workspace-relative paths by next_build's staging_srcs attr. Next.js SWC
  // sees them as regular source files via relative imports.
};

export default nextConfig;

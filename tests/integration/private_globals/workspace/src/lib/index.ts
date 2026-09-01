export function describeBuild(): string {
  return `${BUILD_ID} in ${process.cwd()}`;
}

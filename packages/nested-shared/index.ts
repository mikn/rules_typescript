// Workspace package reached through an importer-relative pnpm link:
//   importer tests/npm/nested → link:../../../packages/nested-shared
// npm_translate_lock resolves that against the importer to generate
// alias(name = "nested-shared", actual = "//packages/nested-shared:nested-shared").
export function shout(name: string): string {
    return `HELLO, ${name.toUpperCase()}!`;
}

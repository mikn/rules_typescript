interface Bindings {
  BUILD_ID: string;
  mainModule: typeof import("./stray");
}

declare const BINDINGS: Bindings;

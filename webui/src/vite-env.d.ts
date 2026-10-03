/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_M3M0S_URL: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}

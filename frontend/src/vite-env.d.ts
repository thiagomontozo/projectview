/// <reference types="vite/client" />

// Declares the shape of a CSS-module import. Without this, TypeScript has no
// idea what `import styles from './x.module.css'` yields and every such import
// is an error.
declare module '*.module.css' {
  const classes: Readonly<Record<string, string>>;
  export default classes;
}

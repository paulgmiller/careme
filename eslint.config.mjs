export default [
  {
    ignores: ["internal/static/htmx@*.js"],
  },
  {
    files: ["internal/static/**/*.js"],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: "module",
      globals: {
        console: "readonly",
        document: "readonly",
        File: "readonly",
        DataTransfer: "readonly",
        Image: "readonly",
        navigator: "readonly",
        setTimeout: "readonly",
        URL: "readonly",
        window: "readonly",
      },
    },
    rules: {
      "no-constant-condition": "error",
      "no-redeclare": "error",
      "no-undef": "error",
      "no-unreachable": "error",
      "no-unused-vars": "error",
    },
  },
];

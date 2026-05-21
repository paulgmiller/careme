export default [
  {
    files: ["internal/static/**/*.js"],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: "script",
      globals: {
        console: "readonly",
        document: "readonly",
        setTimeout: "readonly",
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

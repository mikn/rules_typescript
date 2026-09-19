export type Where = {
  dirname: string;
  filename: string;
  metaDirname: string;
};

export const where = (config: Where) => ({
  name: "where",
  config: () => ({
    define: {
      __CONFIG_DIRNAME__: JSON.stringify(config.dirname),
      __CONFIG_FILENAME__: JSON.stringify(config.filename),
      __CONFIG_META_DIRNAME__: JSON.stringify(config.metaDirname),
      __PLUGIN_DIRNAME__: JSON.stringify(__dirname),
    },
  }),
});

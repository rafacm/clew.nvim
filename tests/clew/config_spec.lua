local config = require("clew.config")

describe("clew.config", function()
  after_each(function()
    -- setup() mutates module state; every test starts from the defaults.
    config.setup({})
  end)

  describe("defaults", function()
    it("match what the documentation promises", function()
      local d = config.defaults
      assert.equals(".clew/index.scip", d.index_path)
      assert.equals("never", d.auto_index)
      assert.is_true(d.staleness_check)
      assert.equals("clew", d.server_name)
      assert.is_nil(d.cmd)
      assert.is_nil(d.bin)
    end)

    -- doc/clew.txt, README.md and doc/README.md all list these. They have
    -- drifted from the code before.
    it("attach to the filetypes the producers can index", function()
      assert.same({
        "java",
        "kotlin",
        "typescript",
        "typescriptreact",
        "javascript",
        "javascriptreact",
      }, config.defaults.filetypes)
    end)

    it("list the root markers in reporting order", function()
      assert.same({
        ".clew/config.toml",
        ".clew/index.scip",
        ".gitmodules",
        ".git",
      }, config.defaults.root.markers)
    end)
  end)

  describe("setup", function()
    it("returns the defaults when given nothing", function()
      assert.same(config.defaults, config.setup(nil))
    end)

    it("overrides a scalar and leaves the rest alone", function()
      local cfg = config.setup({ index_path = "build/clew.scip" })
      assert.equals("build/clew.scip", cfg.index_path)
      assert.equals("clew", cfg.server_name)
      assert.same(config.defaults.filetypes, cfg.filetypes)
    end)

    it("merges nested tables rather than replacing them", function()
      local cfg = config.setup({ root = { include = { "java/svc-a" } } })
      assert.same({ "java/svc-a" }, cfg.root.include)
      assert.same(config.defaults.root.markers, cfg.root.markers)
    end)

    -- vim.tbl_deep_extend replaces list-like values wholesale. That is the
    -- behaviour users get, so it is the behaviour documented and pinned here:
    -- setting `filetypes` opts out of the defaults, it does not add to them.
    it("replaces list values instead of appending to them", function()
      local cfg = config.setup({ filetypes = { "java" } })
      assert.same({ "java" }, cfg.filetypes)
    end)

    it("publishes the result on config.options", function()
      config.setup({ server_name = "clew-dev" })
      assert.equals("clew-dev", config.options.server_name)
    end)

    it("does not mutate the defaults", function()
      local before = vim.deepcopy(config.defaults)
      config.setup({
        index_path = "elsewhere.scip",
        filetypes = { "java" },
        root = { markers = { "WORKSPACE" }, include = { "x" } },
      })
      assert.same(before, config.defaults)
    end)

    -- The previous call's options must not leak into the next one, or a second
    -- setup() in a user's config silently inherits the first.
    it("starts from the defaults on every call", function()
      config.setup({ index_path = "first.scip" })
      local cfg = config.setup({ server_name = "second" })
      assert.equals(".clew/index.scip", cfg.index_path)
      assert.equals("second", cfg.server_name)
    end)

    it("accepts a cmd override", function()
      local cfg = config.setup({ cmd = { "docker", "run", "clew", "lsp" } })
      assert.same({ "docker", "run", "clew", "lsp" }, cfg.cmd)
    end)
  end)
end)

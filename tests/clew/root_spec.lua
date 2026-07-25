-- Root detection is the most important module in the plugin, and the one place
-- where the obvious implementation is wrong: Neovim's built-in `root_markers`
-- picks the NEAREST ancestor holding a marker, which in a submodule umbrella
-- starts one language server per submodule and breaks navigation between them.
--
-- These tests pin the outermost-wins rule and the explicit pin that overrides it.

local helpers = require("tests.helpers")
local root = require("clew.root")

local MARKERS = { ".clew/config.toml", ".clew/index.scip", ".gitmodules", ".git" }

describe("clew.root", function()
  local dir

  after_each(function()
    helpers.rmtree(dir)
    dir = nil
  end)

  describe("single repository", function()
    it("finds the repository root from a nested file", function()
      dir = helpers.tree({ ".git/HEAD", "src/main/java/com/example/Foo.java" })
      local got, marker = root.find(vim.fs.joinpath(dir, "src/main/java/com/example/Foo.java"), MARKERS)
      assert.equals(dir, got)
      assert.equals(".git", marker)
    end)

    it("accepts a directory as the starting point", function()
      dir = helpers.tree({ ".git/HEAD", "src/" })
      assert.equals(dir, root.find(vim.fs.joinpath(dir, "src"), MARKERS))
    end)

    it("resolves from the root itself", function()
      dir = helpers.tree({ ".git/HEAD" })
      assert.equals(dir, root.find(dir, MARKERS))
    end)

    it("returns nil when no marker exists anywhere above", function()
      dir = helpers.tree({ "src/Foo.java" })
      local got, marker = root.find(vim.fs.joinpath(dir, "src/Foo.java"), MARKERS)
      assert.is_nil(got)
      assert.is_nil(marker)
    end)
  end)

  describe("superproject", function()
    -- The case the built-in resolver gets wrong. Both the umbrella and the
    -- submodule carry a marker; only the umbrella is the right answer.
    it("picks the outermost marker, not the nearest", function()
      dir = helpers.tree({
        ".gitmodules",
        ".git/HEAD",
        "java/svc-a/.git",
        "java/svc-a/pom.xml",
        "java/svc-a/src/main/java/A.java",
      })
      local got = root.find(vim.fs.joinpath(dir, "java/svc-a/src/main/java/A.java"), MARKERS)
      assert.equals(dir, got)
    end)

    it("reports the marker it matched at the outermost directory", function()
      dir = helpers.tree({ ".gitmodules", "java/svc-a/.git", "java/svc-a/A.java" })
      local _, marker = root.find(vim.fs.joinpath(dir, "java/svc-a/A.java"), MARKERS)
      -- .gitmodules precedes .git in MARKERS, and both sit at the umbrella.
      assert.equals(".gitmodules", marker)
    end)

    it("resolves every submodule to the same root", function()
      dir = helpers.tree({
        ".gitmodules",
        "java/svc-a/.git", "java/svc-a/A.java",
        "java/svc-b/.git", "java/svc-b/B.java",
        "web/.git", "web/src/index.ts",
      })
      for _, file in ipairs({ "java/svc-a/A.java", "java/svc-b/B.java", "web/src/index.ts" }) do
        assert.equals(dir, root.find(vim.fs.joinpath(dir, file), MARKERS),
          "expected " .. file .. " to resolve to the umbrella")
      end
    end)
  end)

  describe("explicit pin", function()
    -- `.clew/config.toml` exists so a user can pin a root when the heuristic
    -- guesses wrong: a nested umbrella, a monorepo inside a monorepo.
    it("wins over an outer marker", function()
      dir = helpers.tree({
        ".git/HEAD",
        "inner/.clew/config.toml",
        "inner/src/Foo.java",
      })
      local got, marker = root.find(vim.fs.joinpath(dir, "inner/src/Foo.java"), MARKERS)
      assert.equals(vim.fs.joinpath(dir, "inner"), got)
      assert.equals(".clew/config.toml", marker)
    end)

    it("uses the outermost pin when several exist", function()
      dir = helpers.tree({
        "outer/.clew/config.toml",
        "outer/inner/.clew/config.toml",
        "outer/inner/src/Foo.java",
      })
      local got = root.find(vim.fs.joinpath(dir, "outer/inner/src/Foo.java"), MARKERS)
      assert.equals(vim.fs.joinpath(dir, "outer"), got)
    end)

    it("does not apply below the file being resolved", function()
      dir = helpers.tree({
        ".git/HEAD",
        "src/Foo.java",
        "unrelated/.clew/config.toml",
      })
      assert.equals(dir, root.find(vim.fs.joinpath(dir, "src/Foo.java"), MARKERS))
    end)
  end)

  describe("marker configuration", function()
    it("honours a custom marker list", function()
      dir = helpers.tree({ "WORKSPACE", "pkg/Foo.java" })
      local got, marker = root.find(vim.fs.joinpath(dir, "pkg/Foo.java"), { "WORKSPACE" })
      assert.equals(dir, got)
      assert.equals("WORKSPACE", marker)
    end)

    it("finds nothing with an empty marker list", function()
      dir = helpers.tree({ ".git/HEAD", "src/Foo.java" })
      assert.is_nil(root.find(vim.fs.joinpath(dir, "src/Foo.java"), {}))
    end)

    -- An index alone is enough: a user may ship .clew/index.scip without a
    -- config, and clew must still attach.
    it("treats a bare index as a marker", function()
      dir = helpers.tree({ ".clew/index.scip", "src/Foo.java" })
      local got, marker = root.find(vim.fs.joinpath(dir, "src/Foo.java"), MARKERS)
      assert.equals(dir, got)
      assert.equals(".clew/index.scip", marker)
    end)
  end)

  describe("root_dir_fn", function()
    it("resolves the buffer's file", function()
      dir = helpers.tree({ ".git/HEAD", "src/Foo.java" })
      local buf = vim.api.nvim_create_buf(false, true)
      vim.api.nvim_buf_set_name(buf, vim.fs.joinpath(dir, "src/Foo.java"))

      local got
      root.root_dir_fn(MARKERS)(buf, function(d) got = d end)
      assert.equals(dir, got)
      vim.api.nvim_buf_delete(buf, { force = true })
    end)

    -- An unnamed scratch buffer has no path to resolve from, and must not
    -- attach the server to whatever the cwd happens to be.
    it("returns nil for an unnamed buffer", function()
      local buf = vim.api.nvim_create_buf(false, true)
      local called, got = false, "unset"
      root.root_dir_fn(MARKERS)(buf, function(d)
        called = true
        got = d
      end)
      assert.is_true(called)
      assert.is_nil(got)
      vim.api.nvim_buf_delete(buf, { force = true })
    end)

    -- root_dir is documented as returning a single value; find() returns two,
    -- and leaking the marker into on_dir would hand Neovim a stray argument.
    it("passes exactly one argument to the callback", function()
      dir = helpers.tree({ ".git/HEAD", "src/Foo.java" })
      local buf = vim.api.nvim_create_buf(false, true)
      vim.api.nvim_buf_set_name(buf, vim.fs.joinpath(dir, "src/Foo.java"))

      local argc
      root.root_dir_fn(MARKERS)(buf, function(...) argc = select("#", ...) end)
      assert.equals(1, argc)
      vim.api.nvim_buf_delete(buf, { force = true })
    end)
  end)
end)

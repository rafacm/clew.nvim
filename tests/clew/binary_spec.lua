-- Two different things are being located, and conflating them is a bug the
-- plugin already shipped once: `cmd` is the LSP server argv, `bin` is the
-- binary used to BUILD an index. These tests pin the resolution order and the
-- separation between the two.

local helpers = require("tests.helpers")
local binary = require("clew.binary")

local uv = vim.uv or vim.loop

--- The repository root, derived the same way binary.lua derives it.
local function repo_root()
  local this = debug.getinfo(1, "S").source:sub(2)
  return vim.fn.fnamemodify(this, ":p:h:h:h")
end

--- Write an executable file and return its path.
local function fake_executable(dir, name)
  local path = vim.fs.joinpath(dir, name)
  local fd = assert(uv.fs_open(path, "w", 493)) -- 0755
  uv.fs_write(fd, "#!/bin/sh\nexit 0\n")
  uv.fs_close(fd)
  return path
end

describe("clew.binary", function()
  describe("find", function()
    it("prefers an explicit bin", function()
      local path, source = binary.find({ bin = "/opt/clew", cmd = { "/other/clew", "lsp" } })
      assert.equals("/opt/clew", path)
      assert.equals("config bin", source)
    end)

    it("falls back to cmd[1] when there is no bin", function()
      local path, source = binary.find({ cmd = { "/other/clew", "lsp" } })
      assert.equals("/other/clew", path)
      assert.equals("config cmd[1]", source)
    end)

    -- cmd[1] is right for the common {"/path/to/clew", "lsp"} shape and wrong
    -- for a wrapper. :ClewStatus reports the source precisely so a user can see
    -- which of the two they got.
    it("reports cmd[1] as the source even for a wrapper command", function()
      local path, source = binary.find({ cmd = { "docker", "run", "clew", "lsp" } })
      assert.equals("docker", path)
      assert.equals("config cmd[1]", source)
    end)

    it("finds clew on $PATH when the config says nothing", function()
      helpers.with_empty_path(function(dir)
        local want = fake_executable(dir, "clew")
        local path, source = binary.find({})
        assert.equals(want, path)
        assert.equals("$PATH", source)
      end)
    end)

    it("searches $PATH when no config is supplied at all", function()
      helpers.with_empty_path(function(dir)
        local want = fake_executable(dir, "clew")
        assert.equals(want, (binary.find(nil)))
      end)
    end)

    -- lazy.nvim's `build = "make"` leaves the binary in the plugin directory
    -- rather than on $PATH, which is the default installation shape.
    it("falls back to the binary built in the plugin directory", function()
      helpers.with_empty_path(function()
        local candidate = vim.fs.joinpath(repo_root(), "bin", "clew")
        local created = false
        if not uv.fs_stat(candidate) then
          vim.fn.mkdir(vim.fs.dirname(candidate), "p")
          fake_executable(vim.fs.dirname(candidate), "clew")
          created = true
        end

        local path, source = binary.find({})
        local ok, err = pcall(function()
          assert.equals(candidate, path)
          assert.equals("plugin build directory", source)
        end)

        if created then vim.fn.delete(candidate) end
        if not ok then error(err) end
      end)
    end)
  end)

  describe("server_cmd", function()
    it("returns an explicit cmd verbatim", function()
      local cmd = { "docker", "run", "--rm", "clew:latest", "lsp" }
      assert.same(cmd, binary.server_cmd({ cmd = cmd }))
    end)

    it("appends the lsp subcommand to a resolved binary", function()
      assert.same({ "/opt/clew", "lsp" }, binary.server_cmd({ bin = "/opt/clew" }))
    end)

    it("returns nil when no binary can be found", function()
      local saved = binary.find
      binary.find = function() return nil, nil end
      local got = binary.server_cmd({})
      binary.find = saved
      assert.is_nil(got)
    end)
  end)

  describe("index_cmd", function()
    it("builds an index invocation from the resolved binary", function()
      assert.same(
        { "/opt/clew", "index", "--root", "/proj", "--output", ".clew/index.scip" },
        binary.index_cmd({ bin = "/opt/clew", index_path = ".clew/index.scip" }, "/proj")
      )
    end)

    it("passes a unit through", function()
      assert.same(
        { "/opt/clew", "index", "--root", "/proj", "--output", "i.scip", "--unit", "java/svc-a" },
        binary.index_cmd({ bin = "/opt/clew", index_path = "i.scip" }, "/proj", "java/svc-a")
      )
    end)

    it("ignores an empty unit", function()
      local cmd = binary.index_cmd({ bin = "/opt/clew", index_path = "i.scip" }, "/proj", "")
      assert.is_nil(vim.tbl_contains(cmd, "--unit") and true or nil)
    end)

    -- A wrapper cmd ends in `lsp`, and its subcommand is not at a known
    -- position, so index_cmd must never reuse it: it resolves a binary and
    -- appends `index` itself.
    it("does not reuse the server command", function()
      local cmd = binary.index_cmd(
        { cmd = { "/opt/clew", "lsp" }, index_path = "i.scip" },
        "/proj"
      )
      assert.same({ "/opt/clew", "index", "--root", "/proj", "--output", "i.scip" }, cmd)
      assert.is_false(vim.tbl_contains(cmd, "lsp"))
    end)

    it("returns nil when no binary can be found", function()
      local saved = binary.find
      binary.find = function() return nil, nil end
      local got = binary.index_cmd({ index_path = "i.scip" }, "/proj")
      binary.find = saved
      assert.is_nil(got)
    end)
  end)
end)

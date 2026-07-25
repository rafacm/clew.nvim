local M = {}

local uv = vim.uv or vim.loop

local start = vim.health.start or vim.health.report_start
local ok = vim.health.ok or vim.health.report_ok
local warn = vim.health.warn or vim.health.report_warn
local err = vim.health.error or vim.health.report_error
local info = vim.health.info or vim.health.report_info

function M.check()
  local config = require("clew.config")
  local binary = require("clew.binary")
  local root = require("clew.root")
  local cfg = config.options

  start("clew: neovim")
  if vim.fn.has("nvim-0.11") == 1 then
    ok("Neovim 0.11+ (vim.lsp.config available)")
  else
    err("clew requires Neovim 0.11+", { "Upgrade Neovim, or pin clew.nvim to a 0.10-compatible tag." })
  end

  start("clew: binary")
  local bin, source = binary.find(cfg)
  if bin then
    ok(("found: %s (%s)"):format(bin, source))
    local res = vim.system({ bin, "--version" }, { text = true }):wait()
    if res.code == 0 then
      info("version: " .. vim.trim(res.stdout or "?"))
    else
      warn("binary found but `clew --version` failed")
    end
  else
    err("clew binary not found", {
      "Run `make` in the plugin directory, or install clew onto $PATH.",
      "With lazy.nvim: add `build = \"make\"` to the plugin spec.",
    })
  end

  start("clew: toolchains")
  local function tool(name, why)
    if vim.fn.executable(name) == 1 then
      ok(("%s available"):format(name))
    else
      warn(("%s not found -- %s"):format(name, why))
    end
  end
  tool("java", "required to index Java/Kotlin units (JDK 17+)")
  tool("mvn", "required for Maven dependency resolution")
  tool("node", "required to index TypeScript/JavaScript units")

  start("clew: project")
  local name = vim.api.nvim_buf_get_name(0)
  if name == "" then name = uv.cwd() end
  local project, marker = root.find(name, cfg.root.markers)
  if project then
    ok(("root: %s"):format(project))
    info(("matched marker: %s"):format(marker))
    local index = vim.fs.joinpath(project, cfg.index_path)
    local stat = uv.fs_stat(index)
    if stat then
      ok(("index present (%.1f MB)"):format(stat.size / 1024 / 1024))
    else
      warn("no index yet", { "Run :ClewIndex to build one." })
    end
  else
    warn("no clew project root found from the current buffer", {
      "clew looks for the OUTERMOST directory containing one of: "
        .. table.concat(cfg.root.markers, ", "),
      "Create .clew/config.toml at the desired root to pin it explicitly.",
    })
  end
end

return M

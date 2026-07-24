--- Locating the `clew` binary.
---
--- Resolution order:
---   1. an explicit `cmd` in the user's config
---   2. `clew` on $PATH
---   3. a binary built in-tree by `make` (lazy.nvim `build = "make"`)

local M = {}

local uv = vim.uv or vim.loop

--- Directory this plugin was installed into.
---@return string|nil
local function plugin_root()
  local source = debug.getinfo(1, "S").source:sub(2)
  -- .../clew.nvim/lua/clew/binary.lua -> .../clew.nvim
  return vim.fs.dirname(vim.fs.dirname(vim.fs.dirname(source)))
end

--- Absolute path to the clew binary, or nil if it cannot be found.
---@return string|nil path
---@return string|nil source_description
function M.find()
  local on_path = vim.fn.exepath("clew")
  if on_path ~= "" then return on_path, "$PATH" end

  local root = plugin_root()
  if root then
    local candidate = vim.fs.joinpath(root, "bin", "clew")
    if uv.fs_stat(candidate) then return candidate, "plugin build directory" end
  end

  return nil, nil
end

--- The command clew should be started with.
---@param cfg ClewConfig
---@return string[]|nil
function M.server_cmd(cfg)
  if cfg.cmd then return cfg.cmd end
  local path = M.find()
  if not path then return nil end
  return { path, "lsp" }
end

--- The command used to (re)build an index.
---@param cfg ClewConfig
---@param root string
---@param unit string|nil
---@return string[]|nil
function M.index_cmd(cfg, root, unit)
  local path = M.find()
  if not path then return nil end
  local cmd = { path, "index", "--root", root, "--output", cfg.index_path }
  if unit and unit ~= "" then
    table.insert(cmd, "--unit")
    table.insert(cmd, unit)
  end
  return cmd
end

return M

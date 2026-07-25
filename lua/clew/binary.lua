--- Locating the `clew` binary.
---
--- Two things are being located, and conflating them is a bug we already shipped.
---
--- `cmd` is the LSP server command: a complete argv ending in the `lsp`
--- subcommand, handed straight to |vim.lsp.config|. `bin` is the binary used to
--- BUILD an index, which needs the `index` subcommand and its own flags.
---
--- `cmd` cannot simply be reused for indexing. Its subcommand is not at a known
--- position -- `cmd` may be a wrapper such as `{"docker", "run", ..., "clew",
--- "lsp"}` -- so there is no safe way to swap `lsp` for `index`.
---
--- Binary resolution order:
---   1. an explicit `bin` in the user's config
---   2. `cmd[1]`, when `cmd` overrides the server. Correct for the common
---      `{"/path/to/clew", "lsp"}` shape, wrong for a wrapper, which is why
---      `bin` exists and why |:ClewStatus| reports which source was used
---   3. `clew` on $PATH
---   4. a binary built in-tree by `make` (lazy.nvim `build = "make"`)

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
---@param cfg ClewConfig|nil  Omitted only by callers with no config to hand.
---@return string|nil path
---@return string|nil source_description
function M.find(cfg)
  if cfg then
    if cfg.bin then return cfg.bin, "config bin" end
    if cfg.cmd and cfg.cmd[1] then return cfg.cmd[1], "config cmd[1]" end
  end

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
  local path = M.find(cfg)
  if not path then return nil end
  return { path, "lsp" }
end

--- The command used to (re)build an index.
---@param cfg ClewConfig
---@param root string
---@param unit string|nil
---@return string[]|nil
function M.index_cmd(cfg, root, unit)
  local path = M.find(cfg)
  if not path then return nil end
  local cmd = { path, "index", "--root", root, "--output", cfg.index_path }
  if unit and unit ~= "" then
    table.insert(cmd, "--unit")
    table.insert(cmd, unit)
  end
  return cmd
end

return M

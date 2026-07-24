--- LSP registration.
---
--- clew is an ordinary stdio language server, so everything Neovim already knows
--- how to do -- `gd`, `gr`, `<C-]>` via `vim.lsp.tagfunc`, Telescope pickers,
--- aerial.nvim -- works with no plugin-specific glue.

local config = require("clew.config")
local root = require("clew.root")
local binary = require("clew.binary")

local M = {}

local registered = false

---@param cfg ClewConfig
---@return boolean ok
---@return string|nil err
function M.register(cfg)
  if registered then return true end

  local cmd = binary.server_cmd(cfg)
  if not cmd then
    return false, "clew binary not found (not on $PATH and not built in the plugin directory)"
  end

  if not vim.lsp.config then
    return false, "clew requires Neovim 0.11+ (vim.lsp.config is unavailable)"
  end

  vim.lsp.config(cfg.server_name, {
    cmd = cmd,
    filetypes = cfg.filetypes,
    -- NOTE: deliberately a function, not `root_markers`. See lua/clew/root.lua --
    -- the built-in marker search returns the NEAREST match, which would start one
    -- server per submodule.
    root_dir = root.root_dir_fn(cfg.root.markers),
    settings = {
      clew = {
        indexPath = cfg.index_path,
        stalenessCheck = cfg.staleness_check,
      },
    },
  })

  vim.lsp.enable(cfg.server_name)
  registered = true
  return true
end

--- Active clew clients, optionally scoped to a buffer.
---@param bufnr integer|nil
---@return vim.lsp.Client[]
function M.clients(bufnr)
  return vim.lsp.get_clients({ name = config.options.server_name, bufnr = bufnr })
end

--- Stop every clew client. They are restarted lazily on the next matching buffer.
---@return integer stopped
function M.stop()
  local clients = M.clients()
  for _, client in ipairs(clients) do
    client:stop(true)
  end
  return #clients
end

return M

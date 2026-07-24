---@class ClewRootConfig
---@field markers string[] Ordered by priority; clew always picks the OUTERMOST match.
---@field include string[] Explicit unit paths. Empty means auto-discover build roots.
---@field exclude string[] Glob patterns of paths never treated as units.

---@class ClewConfig
---@field index_path string Index location, relative to the project root.
---@field filetypes string[] Filetypes clew attaches to.
---@field root ClewRootConfig
---@field auto_index "never"|"save"|"manual"
---@field staleness_check boolean Warn when the index predates the working tree.
---@field cmd string[]|nil Override the server command entirely.
---@field server_name string Name registered with vim.lsp.config.

local M = {}

---@type ClewConfig
M.defaults = {
  index_path = ".clew/index.scip",

  filetypes = {
    "java",
    "kotlin",
    "typescript",
    "typescriptreact",
    "javascript",
    "javascriptreact",
  },

  root = {
    -- Order matters only for reporting; selection always prefers the outermost
    -- directory containing ANY of these. See lua/clew/root.lua for why.
    markers = {
      ".clew/config.toml",
      ".clew/index.scip",
      ".gitmodules",
      ".git",
    },
    include = {},
    exclude = { "vendor/**", "third_party/**", "node_modules/**" },
  },

  auto_index = "never",
  staleness_check = true,

  cmd = nil,
  server_name = "clew",
}

---@type ClewConfig
M.options = vim.deepcopy(M.defaults)

---@param opts ClewConfig|nil
---@return ClewConfig
function M.setup(opts)
  M.options = vim.tbl_deep_extend("force", vim.deepcopy(M.defaults), opts or {})
  return M.options
end

return M

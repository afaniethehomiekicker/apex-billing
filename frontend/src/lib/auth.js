import { createContext, useContext } from 'react';

// The signed-in user, shared with every page: { user, setUser, can(perm), signOut }.
export const AuthContext = createContext(null);
export const useAuth = () => useContext(AuthContext);

// Go to a page: navigate('clients'), navigate('clients/12').
export const navigate = (path) => { window.location.hash = `#/${path}`; };

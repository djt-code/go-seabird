package auth

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"strings"

	"bitbucket.org/belak/irc"
	"bitbucket.org/belak/irc/mux"
	"labix.org/v2/mgo"
	"labix.org/v2/mgo/bson"
)

type genericAccount struct {
	Id    bson.ObjectId `bson:"_id"`
	Name  string        `bson:"name"`
	Perms []string      `bson:"perms,omitempty"`
}

type user struct {
	CurrentNick string
	Account     string
	Channels    []string
}

type GenericAuth struct {
	Client *irc.Client
	C      *mgo.Collection
	users  map[string]*user
	Salt   string
}

func (au *GenericAuth) userCan(u *user, p string) bool {
	if u.Account == "" {
		return false
	}

	c, err := au.C.Find(bson.M{
		"name":  u.Account,
		"perms": p,
	}).Count()

	if err != nil {
		fmt.Println(err)
		return false
	}

	return c > 0
}

func (au *GenericAuth) getHash() hash.Hash {
	h := md5.New()
	io.WriteString(h, au.Salt)
	return h
}

func (au *GenericAuth) newLoginHandler(prefix string) irc.HandlerFunc {
	return func(c *irc.Client, e *irc.Event) {
		u := au.getUser(e.Identity.Nick)
		if u.Account != "" {
			c.MentionReply(e, "you are already logged in as '%s'", u.Account)
			return
		}

		if len(u.Channels) == 0 {
			c.MentionReply(e, "You cannot log in if you're not in a channel with me")
			return
		}

		args := strings.SplitN(e.Trailing(), " ", 2)
		if len(args) != 2 {
			c.MentionReply(e, "usage: %slogin username password", prefix)
			return
		}

		h := au.getHash()
		io.WriteString(h, args[1])

		pw := hex.EncodeToString(h.Sum(nil))

		cnt, err := au.C.Find(bson.M{
			"name":     args[0],
			"password": pw,
		}).Count()

		if err != nil {
			fmt.Println(err)
			return
		}

		if cnt > 0 {
			u.Account = args[0]
			au.Client.MentionReply(e, "you are now logged in as '%s'", args[0])
			au.users[u.CurrentNick] = u
		} else {
			au.Client.MentionReply(e, "login failed")
		}
	}
}

func (au *GenericAuth) newLogoutHandler(prefix string) irc.HandlerFunc {
	return func(c *irc.Client, e *irc.Event) {
		u := au.getUser(e.Identity.Nick)
		if u.Account == "" {
			c.MentionReply(e, "you are not logged in")
			return
		}

		u.Account = ""
		au.users[u.CurrentNick] = u
		c.MentionReply(e, "you have been logged out")
	}
}

func (au *GenericAuth) newRegisterHandler(prefix string) irc.HandlerFunc {
	return func(c *irc.Client, e *irc.Event) {
		u := au.getUser(e.Identity.Nick)
		if u.Account != "" {
			c.MentionReply(e, "you are already logged in as '%s'", u.Account)
			return
		}

		args := strings.SplitN(e.Trailing(), " ", 2)
		if len(args) < 2 {
			c.MentionReply(e, "usage: %sregister <username> <password>", prefix)
			return
		}

		cnt, err := au.C.Find(bson.M{
			"name": args[0],
		}).Count()

		if err != nil {
			fmt.Println(err)
			return
		}

		if cnt > 0 {
			c.MentionReply(e, "there is already a user with that name")
			return
		}

		h := au.getHash()
		io.WriteString(h, args[1])

		err = au.C.Insert(bson.M{
			"name":     args[0],
			"password": hex.EncodeToString(h.Sum(nil)),
		})

		if err != nil {
			fmt.Println(err)
			return
		}

		u.Account = args[0]
		delete(au.users, e.Identity.Nick)
		au.users[e.Identity.Nick] = u

		c.MentionReply(e, "you have been registered and logged in")
	}
}

func (au *GenericAuth) newAddPermHandler(prefix string) irc.HandlerFunc {
	return func(c *irc.Client, e *irc.Event) {
		u := au.getUser(e.Identity.Nick)
		if u.Account == "" {
			c.MentionReply(e, "you are not logged in")
			return
		}

		if !au.userCan(u, "admin") && !au.userCan(u, "generic_auth.addperm") {
			c.MentionReply(e, "you don't have permission to add permissions")
			return
		}

		args := strings.Split(e.Trailing(), " ")
		if len(args) != 2 {
			c.MentionReply(e, "usage: %saddperm <user> <perm>", prefix)
			return
		}

		a := genericAccount{}
		err := au.C.Find(bson.M{"name": args[0]}).One(&a)
		if err != nil {
			// NOTE: This may be another error?
			c.MentionReply(e, "account '%s' does not exist", args[0])
			return
		}

		if args[1] == "admin" && !au.userCan(u, "admin") {
			c.MentionReply(e, "only users with the 'admin' permission can add admins")
			return
		}

		for _, v := range a.Perms {
			if v == args[1] {
				c.MentionReply(e, "user '%s' already has perm '%s'", args[0], args[1])
				return
			}
		}

		au.C.UpdateId(a.Id, bson.M{"$push": bson.M{"perms": args[1]}})
		c.MentionReply(e, "added perm '%s' to user '%s'", args[1], args[0])
	}
}

func (au *GenericAuth) newDelPermHandler(prefix string) irc.HandlerFunc {
	return func(c *irc.Client, e *irc.Event) {
		u := au.getUser(e.Identity.Nick)
		if u.Account == "" {
			c.MentionReply(e, "you are not logged in")
			return
		}

		if !au.userCan(u, "admin") && !au.userCan(u, "generic_auth.delperm") {
			c.MentionReply(e, "you don't have permission to remove permissions")
			return
		}

		args := strings.Split(e.Trailing(), " ")
		if len(args) != 2 {
			c.MentionReply(e, "usage: %sdelperm <user> <perm>", prefix)
			return
		}

		if args[1] == "admin" && !au.userCan(u, "admin") {
			c.MentionReply(e, "only users with the 'admin' permission can remove admins")
			return
		}

		err := au.C.Update(bson.M{"name": args[0]}, bson.M{"$pull": bson.M{"perms": args[1]}})
		if err != nil {
			c.MentionReply(e, "account '%s' does not exist", args[0])
			return
		}

		c.MentionReply(e, "removed perm '%s' to user '%s'", args[1], args[0])
	}
}

func (au *GenericAuth) newCheckPermHandler(prefix string) irc.HandlerFunc {
	return func(c *irc.Client, e *irc.Event) {
		u := au.getUser(e.Identity.Nick)
		if u.Account == "" {
			c.MentionReply(e, "you are not logged in")
			return
		}

		if !au.userCan(u, "admin") && !au.userCan(u, "generic_auth.checkperms") {
			c.MentionReply(e, "you do not have permission to view permissions")
			return
		}

		args := strings.Split(e.Trailing(), " ")
		if len(args) != 1 {
			c.MentionReply(e, "usage: %scheckperms <user>", prefix)
			return
		}

		a := genericAccount{}
		err := au.C.Find(bson.M{"name": args[0]}).One(&a)
		if err != nil {
			c.MentionReply(e, "account '%s' does not exist", args[0])
			return
		}

		c.MentionReply(e, "permissions for '%s': %s", args[0], strings.Join(a.Perms, ", "))
	}
}

func (au *GenericAuth) newWhoisHandler(prefix string) irc.HandlerFunc {
	return func(c *irc.Client, e *irc.Event) {
		u := au.getUser(e.Identity.Nick)
		if u.Account == "" {
			c.MentionReply(e, "you are not logged in")
			return
		}

		if !au.userCan(u, "admin") && !au.userCan(u, "generic_auth.whois") {
			c.MentionReply(e, "you do not have permission to check a user account")
			return
		}

		args := strings.Split(e.Trailing(), " ")
		if len(args) != 1 || args[0] == "" {
			c.MentionReply(e, "usage: %swhois <user>", prefix)
			return
		}

		if cu, ok := au.users[args[0]]; ok && cu.Account != "" {
			c.MentionReply(e, "nick '%s' is user '%s'", args[0], cu.Account)
		} else {
			c.MentionReply(e, "nick '%s' is not logged in", args[0])
		}
	}
}

func (au *GenericAuth) newPasswdHandler(prefix string) irc.HandlerFunc {
	return func(c *irc.Client, e *irc.Event) {
		u := au.getUser(e.Identity.Nick)
		if u.Account == "" {
			c.MentionReply(e, "you are not logged in")
			return
		}

		args := strings.Split(e.Trailing(), " ")
		if len(args) != 1 || args[0] == "" {
			c.MentionReply(e, "usage: %spasswd <newpass>", prefix)
			return
		}

		h := au.getHash()
		io.WriteString(h, args[0])

		_, err := au.C.Upsert(bson.M{"name": u.Account}, bson.M{"$set": bson.M{"password": hex.EncodeToString(h.Sum(nil))}})
		if err != nil {
			fmt.Println(err)
			return
		}

		c.MentionReply(e, "your password has been changed")
	}
}

func NewGenericAuth(c *irc.Client, db *mgo.Database, prefix string, salt string) *GenericAuth {
	au := &GenericAuth{Client: c, C: db.C("generic_auth_accounts"), Salt: salt}
	au.trackUsers()

	cmds := mux.NewCommandMux(prefix)
	cmds.PrivateFunc("login", au.newLoginHandler(prefix))
	cmds.PrivateFunc("logout", au.newLogoutHandler(prefix))
	cmds.PrivateFunc("register", au.newRegisterHandler(prefix))
	cmds.PrivateFunc("addperm", au.newAddPermHandler(prefix))
	cmds.PrivateFunc("delperm", au.newDelPermHandler(prefix))
	cmds.PrivateFunc("checkperms", au.newCheckPermHandler(prefix))
	cmds.PrivateFunc("whois", au.newWhoisHandler(prefix))
	cmds.PrivateFunc("passwd", au.newPasswdHandler(prefix))
	c.Event("PRIVMSG", cmds)

	return au
}

type genericAuthHandler struct {
	au *GenericAuth
	h  irc.Handler
	p  string
}

func (h genericAuthHandler) HandleEvent(c *irc.Client, e *irc.Event) {
	u := h.au.getUser(e.Identity.Nick)
	if h.au.userCan(u, h.p) {
		h.h.HandleEvent(c, e)
	} else {
		c.MentionReply(e, "You do not have the required permissions: %s", h.p)
	}
}

func (au *GenericAuth) CheckPerm(p string, h irc.Handler) irc.Handler {
	return genericAuthHandler{au: au, h: h, p: p}
}

func (au *GenericAuth) CheckPermFunc(p string, f irc.HandlerFunc) irc.HandlerFunc {
	return func(c *irc.Client, e *irc.Event) {
		u := au.getUser(e.Identity.Nick)
		if au.userCan(u, p) {
			f(c, e)
		} else {
			c.MentionReply(e, "You do not have the required permission: %s", p)
		}
	}
}

// user tracking utilities

func (au *GenericAuth) getUser(nick string) *user {
	u, ok := au.users[nick]
	if !ok {
		u = &user{CurrentNick: nick}
	}

	return u
}

func (au *GenericAuth) addChannelToNick(c, n string) {
	u := au.getUser(n)

	for i := 0; i < len(u.Channels); i++ {
		if u.Channels[i] == c {
			return
		}
	}

	u.Channels = append(u.Channels, c)
	au.users[n] = u
}

func (au *GenericAuth) removeChannelFromUser(c string, u *user) {
	for i := 0; i < len(u.Channels); i++ {
		if u.Channels[i] == c {
			// Swap with last element and shrink slice
			u.Channels[i] = u.Channels[len(u.Channels)-1]
			u.Channels = u.Channels[:len(u.Channels)-1]
			break
		}
	}

	if len(u.Channels) == 0 {
		// Removing user
		delete(au.users, u.CurrentNick)
	}
}

// user tracking

func (au *GenericAuth) connectHandler(c *irc.Client, e *irc.Event) {
	au.users = make(map[string]*user)
}

func (au *GenericAuth) joinHandler(c *irc.Client, e *irc.Event) {
	if e.Identity.Nick != c.CurrentNick() {
		au.addChannelToNick(e.Args[0], e.Identity.Nick)
	} else {
		for _, user := range au.users {
			au.removeChannelFromUser(e.Args[0], user)
		}
	}
}

func (au *GenericAuth) nickHandler(c *irc.Client, e *irc.Event) {
	u := au.getUser(e.Identity.Nick)
	if len(u.Channels) == 0 {
		return
	}

	u.CurrentNick = e.Trailing()
	au.users[u.CurrentNick] = u

	u = au.getUser(u.CurrentNick)
	fmt.Println(u.CurrentNick)
}

func (au *GenericAuth) partHandler(c *irc.Client, e *irc.Event) {
	if e.Identity.Nick != c.CurrentNick() {
		if u, ok := au.users[e.Identity.Nick]; ok {
			au.removeChannelFromUser(e.Args[0], u)
		}
	} else {
		for _, u := range au.users {
			au.removeChannelFromUser(e.Args[0], u)
		}
	}
}

func (au *GenericAuth) quitHandler(c *irc.Client, e *irc.Event) {
	if e.Identity.Nick == c.CurrentNick() {
		// nop
		return
	}

	delete(au.users, e.Identity.Nick)
}

func (au *GenericAuth) namreplyHandler(c *irc.Client, e *irc.Event) {
	ch := e.Args[len(e.Args)-2]
	args := strings.Split(e.Trailing(), " ")

	for i := 0; i < len(args); i++ {
		n := args[i]
		if (n[0] < 'a' || n[0] > 'z') && (n[0] < 'A' || n[0] > 'Z') {
			n = n[1:]
		}

		au.addChannelToNick(ch, n)
	}
}

func (au *GenericAuth) trackUsers() {
	au.Client.EventFunc("001", au.connectHandler)
	au.Client.EventFunc("JOIN", au.joinHandler)
	au.Client.EventFunc("NICK", au.nickHandler)
	au.Client.EventFunc("PART", au.partHandler)
	au.Client.EventFunc("QUIT", au.quitHandler)
	au.Client.EventFunc("353", au.namreplyHandler)
}
